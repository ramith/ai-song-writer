package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	zerologlog "github.com/rs/zerolog/log"
)

// OAuth 2.0 Client Credentials structures
type OAuthTokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope,omitempty"`
}

type OAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
	Error       string `json:"error,omitempty"`
	ErrorDesc   string `json:"error_description,omitempty"`
}

// OAuth Client manages token lifecycle
type OAuthClient struct {
	httpClient    *http.Client
	tokenEndpoint string
	clientID      string
	clientSecret  string
	scope         string

	// Token management
	mutex       sync.RWMutex
	accessToken string
	expiresAt   time.Time
}

// OpenAI API structures for AI Gateway proxy calls
type OpenAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []OpenAIChatMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
}

type OpenAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIChatResponse struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []OpenAIChatChoice   `json:"choices"`
	Usage   OpenAIUsage          `json:"usage"`
	Error   *OpenAIErrorResponse `json:"error,omitempty"`
}

type OpenAIChatChoice struct {
	Index        int               `json:"index"`
	Message      OpenAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIErrorResponse struct {
	Message string      `json:"message"`
	Type    string      `json:"type"`
	Param   interface{} `json:"param"`
	Code    interface{} `json:"code"`
}

// sanitizeForLogging removes sensitive information from strings for logging
func sanitizeForLogging(input string) string {
	if strings.Contains(strings.ToLower(input), "bearer ") {
		return "[REDACTED_BEARER_TOKEN]"
	}
	if strings.HasPrefix(strings.ToLower(input), "sk-") {
		return "[REDACTED_API_KEY]"
	}
	// Redact any string that looks like an API key or token (starts with common prefixes)
	for _, prefix := range []string{"sk-", "pk-", "api_", "token_", "ey", "access_token"} {
		if strings.HasPrefix(strings.ToLower(input), prefix) {
			return "[REDACTED_TOKEN]"
		}
	}
	// Redact client secrets and IDs if they appear in logs
	if len(input) > 20 && (strings.Contains(strings.ToLower(input), "secret") || strings.Contains(strings.ToLower(input), "client")) {
		return "[REDACTED_CREDENTIALS]"
	}
	return input
}

// NewOAuthClient creates a new OAuth client for Client Credentials flow
func NewOAuthClient(tokenEndpoint, clientID, clientSecret, scope string) *OAuthClient {
	return &OAuthClient{
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		tokenEndpoint: tokenEndpoint,
		clientID:      clientID,
		clientSecret:  clientSecret,
		scope:         scope,
	}
}

// GetAccessToken retrieves a valid access token, refreshing if necessary
func (o *OAuthClient) GetAccessToken(ctx context.Context) (string, error) {
	o.mutex.RLock()
	// Check if we have a valid token (with 30 second buffer)
	if o.accessToken != "" && time.Now().Add(30*time.Second).Before(o.expiresAt) {
		token := o.accessToken
		o.mutex.RUnlock()
		return token, nil
	}
	o.mutex.RUnlock()

	// Need to get a new token
	o.mutex.Lock()
	defer o.mutex.Unlock()

	// Double-check after acquiring write lock
	if o.accessToken != "" && time.Now().Add(30*time.Second).Before(o.expiresAt) {
		return o.accessToken, nil
	}

	return o.requestNewToken(ctx)
}

// requestNewToken requests a new access token using Client Credentials flow
func (o *OAuthClient) requestNewToken(ctx context.Context) (string, error) {
	// Prepare form-encoded token request (OAuth 2.0 standard)
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", o.clientID)
	data.Set("client_secret", o.clientSecret)
	if o.scope != "" {
		data.Set("scope", o.scope)
	}

	// Create HTTP request with form data
	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	// Set headers for OAuth token request (form-encoded)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	zerologlog.Debug().
		Str("token_endpoint", o.tokenEndpoint).
		Str("client_id", sanitizeForLogging(o.clientID)).
		Str("scope", o.scope).
		Msg("Requesting OAuth access token")

	// Make token request
	resp, err := o.httpClient.Do(httpReq)
	if err != nil {
		zerologlog.Error().Err(err).
			Str("token_endpoint", o.tokenEndpoint).
			Msg("OAuth token request failed")
		return "", fmt.Errorf("OAuth token request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	// Parse token response
	var tokenResp OAuthTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		zerologlog.Error().Err(err).
			Str("body", string(body)).
			Msg("Failed to parse OAuth token response")
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	// Check for OAuth errors
	if tokenResp.Error != "" {
		zerologlog.Error().
			Str("error", tokenResp.Error).
			Str("error_description", tokenResp.ErrorDesc).
			Int("status_code", resp.StatusCode).
			Msg("OAuth token request error")
		return "", fmt.Errorf("OAuth error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		zerologlog.Error().
			Int("status_code", resp.StatusCode).
			Str("body", string(body)).
			Msg("OAuth token endpoint returned non-200 status")
		return "", fmt.Errorf("OAuth token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	// Validate token response
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("received empty access token")
	}

	// Update stored token with expiration (default to 1 hour if not provided)
	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600 // Default to 1 hour
	}

	o.accessToken = tokenResp.AccessToken
	o.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	zerologlog.Debug().
		Str("token_type", tokenResp.TokenType).
		Int("expires_in", tokenResp.ExpiresIn).
		Time("expires_at", o.expiresAt).
		Msg("Successfully obtained OAuth access token")

	return o.accessToken, nil
}

// promptSystem returns the system prompt for the OpenAI model
func promptSystem() string {
	return "You are a professional songwriter who creates family-friendly, appropriate lyrics for all ages. Always ensure content is positive and suitable for children."
}

// LyricsRequest represents the input for lyrics generation
type LyricsRequest struct {
	Keywords  []string      `json:"keywords" binding:"required,min=1,max=10"`
	Genre     string        `json:"genre" binding:"required"`
	Emotion   string        `json:"emotion" binding:"required"`
	Language  string        `json:"language" binding:"required"`
	Structure SongStructure `json:"structure"`
}

// SongStructure defines the structure of the song
type SongStructure struct {
	Verses int  `json:"verses" binding:"min=1,max=4"`
	Chorus bool `json:"chorus"`
	Bridge bool `json:"bridge"`
}

// LyricsResponse represents the generated lyrics response
type LyricsResponse struct {
	ID       string          `json:"id"`
	Lyrics   GeneratedLyrics `json:"lyrics"`
	Metadata LyricsMetadata  `json:"metadata"`
}

// GeneratedLyrics contains the actual song content
type GeneratedLyrics struct {
	Title     string            `json:"title"`
	Structure map[string]string `json:"structure"`
}

// LyricsMetadata contains information about the generated lyrics
type LyricsMetadata struct {
	Genre        string    `json:"genre"`
	Emotion      string    `json:"emotion"`
	Language     string    `json:"language"`
	KeywordsUsed []string  `json:"keywords_used"`
	CreatedAt    time.Time `json:"created_at"`
	WordCount    int       `json:"word_count"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
}

// ErrorResponse represents error responses
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// LyricsService handles the lyrics generation logic using AI Gateway with OAuth
type LyricsService struct {
	httpClient  *http.Client
	oauthClient *OAuthClient
	gatewayURL  string
	model       string
}

// NewLyricsService creates a new lyrics service with AI Gateway and OAuth
func NewLyricsService(gatewayURL, model string, oauthClient *OAuthClient) *LyricsService {
	return &LyricsService{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		oauthClient: oauthClient,
		gatewayURL:  gatewayURL,
		model:       model,
	}
}

// ValidGenres contains the list of supported genres
var ValidGenres = map[string]bool{
	"pop":        true,
	"rock":       true,
	"country":    true,
	"hip-hop":    true,
	"r&b":        true,
	"jazz":       true,
	"folk":       true,
	"electronic": true,
	"classical":  true,
	"reggae":     true,
	"blues":      true,
	"indie":      true,
}

// ValidEmotions contains the list of supported emotions
var ValidEmotions = map[string]bool{
	"happy":         true,
	"sad":           true,
	"romantic":      true,
	"energetic":     true,
	"melancholic":   true,
	"hopeful":       true,
	"nostalgic":     true,
	"peaceful":      true,
	"excited":       true,
	"contemplative": true,
}

// ValidLanguages contains the list of supported languages
var ValidLanguages = map[string]bool{
	"english":    true,
	"spanish":    true,
	"french":     true,
	"german":     true,
	"italian":    true,
	"portuguese": true,
	"japanese":   true,
	"korean":     true,
}

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		zerologlog.Info().Msg("No .env file found")
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if os.Getenv("GIN_MODE") == "release" {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	// Get AI Gateway OAuth configuration
	consumerKey := os.Getenv("AI_GATEWAY_CONSUMER_KEY")
	if consumerKey == "" {
		zerologlog.Fatal().Msg("AI_GATEWAY_CONSUMER_KEY environment variable is required")
	}

	consumerSecret := os.Getenv("AI_GATEWAY_CONSUMER_SECRET")
	if consumerSecret == "" {
		zerologlog.Fatal().Msg("AI_GATEWAY_CONSUMER_SECRET environment variable is required")
	}

	tokenEndpoint := os.Getenv("AI_GATEWAY_TOKEN_ENDPOINT")
	if tokenEndpoint == "" {
		zerologlog.Fatal().Msg("AI_GATEWAY_TOKEN_ENDPOINT environment variable is required")
	}

	gatewayURL := os.Getenv("AI_GATEWAY_ENDPOINT")
	if gatewayURL == "" {
		zerologlog.Fatal().Msg("AI_GATEWAY_ENDPOINT environment variable is required")
	}

	// Get optional OAuth scope (default to empty)
	oauthScope := os.Getenv("AI_GATEWAY_SCOPE")

	// Get model version (default: gpt-3.5-turbo)
	openaiModel := os.Getenv("OPENAI_MODEL")
	if openaiModel == "" {
		openaiModel = "gpt-3.5-turbo"
	}

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize OAuth client
	oauthClient := NewOAuthClient(tokenEndpoint, consumerKey, consumerSecret, oauthScope)

	zerologlog.Info().
		Str("token_endpoint", tokenEndpoint).
		Str("gateway_url", gatewayURL).
		Str("consumer_key", sanitizeForLogging(consumerKey)).
		Str("model", openaiModel).
		Msg("Initializing AI Gateway with OAuth Client Credentials")

	// Initialize services with AI Gateway
	lyricsService := NewLyricsService(gatewayURL, openaiModel, oauthClient)

	// Setup Gin router
	router := gin.Default()

	// Middleware for CORS
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Health check endpoint
	router.GET("/health", healthCheck)

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		v1.POST("/generate", generateLyrics(lyricsService))
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		zerologlog.Info().Str("port", port).Msg("Starting server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zerologlog.Fatal().Err(err).Msg("Server failed to start")
		}
	}()

	// Set up signal handling for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Wait for interrupt signal
	<-quit
	zerologlog.Info().Msg("Received shutdown signal, gracefully shutting down server...")

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		zerologlog.Error().Err(err).Msg("Server forced to shutdown")
		return
	}

	zerologlog.Info().Msg("Server exited gracefully")
}

// healthCheck returns the health status of the API
func healthCheck(c *gin.Context) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   "1.0.0",
	}
	c.JSON(http.StatusOK, response)
}

// generateLyrics handles the lyrics generation endpoint
func generateLyrics(service *LyricsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req LyricsRequest

		// Bind and validate request
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "invalid_request",
				Message: err.Error(),
			})
			return
		}

		// Validate genre
		if !ValidGenres[strings.ToLower(req.Genre)] {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "invalid_genre",
				Message: "Unsupported genre. Supported genres: " + getValidOptions(ValidGenres),
			})
			return
		}

		// Validate emotion
		if !ValidEmotions[strings.ToLower(req.Emotion)] {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "invalid_emotion",
				Message: "Unsupported emotion. Supported emotions: " + getValidOptions(ValidEmotions),
			})
			return
		}

		// Validate language
		if !ValidLanguages[strings.ToLower(req.Language)] {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "invalid_language",
				Message: "Unsupported language. Supported languages: " + getValidOptions(ValidLanguages),
			})
			return
		}

		// Set default structure if not provided
		if req.Structure.Verses == 0 {
			req.Structure.Verses = 2
		}
		if !req.Structure.Chorus {
			req.Structure.Chorus = true
		}

		// Generate lyrics
		response, err := service.GenerateLyrics(c.Request.Context(), req)
		if err != nil {
			zerologlog.Error().Err(err).Msg("Error generating lyrics")
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error:   "generation_failed",
				Message: "Failed to generate lyrics. Please try again.",
			})
			return
		}

		c.JSON(http.StatusOK, response)
	}
}

// GenerateLyrics generates song lyrics using AI Gateway with OAuth authentication
func (s *LyricsService) GenerateLyrics(ctx context.Context, req LyricsRequest) (*LyricsResponse, error) {
	// Create prompt
	prompt := s.buildPrompt(req)

	// Get OAuth access token
	accessToken, err := s.oauthClient.GetAccessToken(ctx)
	if err != nil {
		zerologlog.Error().Err(err).Msg("Failed to get OAuth access token")
		return nil, fmt.Errorf("OAuth authentication failed: %w", err)
	}

	// Prepare OpenAI request for AI Gateway
	openaiReq := OpenAIChatRequest{
		Model: s.model,
		Messages: []OpenAIChatMessage{
			{Role: "system", Content: promptSystem()},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   1000,
		Temperature: 0.8,
	}

	// Serialize request
	reqBody, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request to AI Gateway
	url := s.gatewayURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers with OAuth Bearer token
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)

	// Log request (with sanitization)
	zerologlog.Debug().
		Str("model", s.model).
		Str("gateway_url", url).
		Str("prompt", sanitizeForLogging(prompt)).
		Str("authorization", sanitizeForLogging("Bearer "+accessToken)).
		Interface("request", req).
		Msg("Sending request to AI Gateway")

	// Make HTTP request
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		zerologlog.Error().Err(err).
			Str("model", s.model).
			Str("gateway_url", url).
			Msg("AI Gateway HTTP request failed")
		return nil, fmt.Errorf("AI Gateway HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var openaiResp OpenAIChatResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		zerologlog.Error().Err(err).
			Str("body", string(body)).
			Msg("Failed to parse AI Gateway response")
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for API errors
	if openaiResp.Error != nil {
		zerologlog.Error().
			Str("error_type", openaiResp.Error.Type).
			Str("error_message", openaiResp.Error.Message).
			Interface("error_code", openaiResp.Error.Code).
			Msg("AI Gateway API error")
		return nil, fmt.Errorf("AI Gateway API error: %s", openaiResp.Error.Message)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		zerologlog.Error().
			Int("status_code", resp.StatusCode).
			Str("body", string(body)).
			Msg("AI Gateway returned non-200 status")
		return nil, fmt.Errorf("AI Gateway returned status %d: %s", resp.StatusCode, string(body))
	}

	// Validate response
	if len(openaiResp.Choices) == 0 {
		zerologlog.Error().
			Str("model", s.model).
			Str("response", string(body)).
			Msg("AI Gateway returned no choices")
		return nil, fmt.Errorf("no response from AI Gateway")
	}

	generatedText := openaiResp.Choices[0].Message.Content
	lyrics := s.parseLyrics(generatedText, req)
	wordCount := s.countWords(generatedText)

	lyricsResponse := &LyricsResponse{
		ID:     uuid.New().String(),
		Lyrics: lyrics,
		Metadata: LyricsMetadata{
			Genre:        req.Genre,
			Emotion:      req.Emotion,
			Language:     req.Language,
			KeywordsUsed: req.Keywords,
			CreatedAt:    time.Now(),
			WordCount:    wordCount,
		},
	}

	zerologlog.Debug().
		Str("response_id", lyricsResponse.ID).
		Int("word_count", wordCount).
		Str("title", lyrics.Title).
		Msg("Successfully generated lyrics via AI Gateway")

	return lyricsResponse, nil
}

// buildPrompt creates the prompt for OpenAI based on the request
func (s *LyricsService) buildPrompt(req LyricsRequest) string {
	keywords := strings.Join(req.Keywords, ", ")

	prompt := fmt.Sprintf(`Write song lyrics in %s with the following specifications:

Genre: %s
Emotion/Mood: %s
Keywords to include: %s
Number of verses: %d
Include chorus: %t
Include bridge: %t

Requirements:
- Family-friendly content only (suitable for all ages)
- No explicit language, violence, or inappropriate themes
- Creative and engaging lyrics that flow well
- Natural incorporation of the provided keywords
- Clear structure with labeled sections

Please format the output with clear section labels like:
[Title: Song Title Here]
[Verse 1]
...
[Chorus]
...
[Verse 2]
...
[Bridge] (if requested)
...

Make sure the lyrics capture the %s emotion and fit the %s genre style.`,
		req.Language, req.Genre, req.Emotion, keywords,
		req.Structure.Verses, req.Structure.Chorus, req.Structure.Bridge,
		req.Emotion, req.Genre,
	)

	return prompt
}

// parseLyrics parses the generated text into structured lyrics
func (s *LyricsService) parseLyrics(text string, req LyricsRequest) GeneratedLyrics {
	lines := strings.Split(text, "\n")
	structure := make(map[string]string)
	title := "Untitled Song"

	currentSection := ""
	currentContent := []string{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if this is a section header
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			// Save previous section
			if currentSection != "" && len(currentContent) > 0 {
				structure[currentSection] = strings.Join(currentContent, "\n")
			}

			// Parse new section
			sectionName := strings.ToLower(strings.Trim(line, "[]"))
			if strings.HasPrefix(sectionName, "title:") {
				title = strings.TrimSpace(strings.TrimPrefix(sectionName, "title:"))
				currentSection = ""
			} else {
				currentSection = sectionName
				currentContent = []string{}
			}
		} else if currentSection != "" {
			currentContent = append(currentContent, line)
		}
	}

	// Save last section
	if currentSection != "" && len(currentContent) > 0 {
		structure[currentSection] = strings.Join(currentContent, "\n")
	}

	// If no structured content found, use the whole text
	if len(structure) == 0 {
		structure["verse1"] = text
	}

	return GeneratedLyrics{
		Title:     title,
		Structure: structure,
	}
}

// countWords counts the number of words in the text
func (s *LyricsService) countWords(text string) int {
	words := strings.Fields(text)
	return len(words)
}

// getValidOptions returns a comma-separated string of valid options
func getValidOptions(validMap map[string]bool) string {
	var options []string
	for key := range validMap {
		options = append(options, key)
	}
	return strings.Join(options, ", ")
}
