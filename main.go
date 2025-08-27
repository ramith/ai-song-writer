package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	zerologlog "github.com/rs/zerolog/log"
)

// OpenAI API structures for direct HTTP calls
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
		return "[REDACTED_API_KEY]"
	}
	if strings.HasPrefix(strings.ToLower(input), "sk-") {
		return "[REDACTED_API_KEY]"
	}
	// Redact any string that looks like an API key (starts with common prefixes)
	for _, prefix := range []string{"sk-", "pk-", "api_", "token_"} {
		if strings.HasPrefix(strings.ToLower(input), prefix) {
			return "[REDACTED_API_KEY]"
		}
	}
	return input
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

// LyricsService handles the lyrics generation logic using direct HTTP calls
type LyricsService struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	model      string
}

// NewLyricsService creates a new lyrics service with direct HTTP API calls
func NewLyricsService(apiKey, baseURL, model string) *LyricsService {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &LyricsService{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     apiKey,
		baseURL:    baseURL,
		model:      model,
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

	// Get OpenAI API key
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		zerologlog.Fatal().Msg("OPENAI_API_KEY environment variable is required")
	}

	// Get custom OpenAI base URL (optional)
	openaiBaseURL := os.Getenv("OPENAI_BASE_URL")

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

	// Initialize services
	lyricsService := NewLyricsService(openaiKey, openaiBaseURL, openaiModel)

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

// GenerateLyrics generates song lyrics using direct OpenAI HTTP API
func (s *LyricsService) GenerateLyrics(ctx context.Context, req LyricsRequest) (*LyricsResponse, error) {
	// Create prompt
	prompt := s.buildPrompt(req)

	// Prepare OpenAI request
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

	// Create HTTP request
	url := s.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)

	// Log request (with sanitization)
	zerologlog.Debug().
		Str("model", s.model).
		Str("url", url).
		Str("prompt", sanitizeForLogging(prompt)).
		Interface("request", req).
		Msg("Sending request to OpenAI")

	// Make HTTP request
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		zerologlog.Error().Err(err).
			Str("model", s.model).
			Str("url", url).
			Msg("HTTP request failed")
		return nil, fmt.Errorf("HTTP request failed: %w", err)
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
			Msg("Failed to parse OpenAI response")
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for API errors
	if openaiResp.Error != nil {
		zerologlog.Error().
			Str("error_type", openaiResp.Error.Type).
			Str("error_message", openaiResp.Error.Message).
			Interface("error_code", openaiResp.Error.Code).
			Msg("OpenAI API error")
		return nil, fmt.Errorf("OpenAI API error: %s", openaiResp.Error.Message)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		zerologlog.Error().
			Int("status_code", resp.StatusCode).
			Str("body", string(body)).
			Msg("OpenAI API returned non-200 status")
		return nil, fmt.Errorf("OpenAI API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Validate response
	if len(openaiResp.Choices) == 0 {
		zerologlog.Error().
			Str("model", s.model).
			Str("response", string(body)).
			Msg("OpenAI API returned no choices")
		return nil, fmt.Errorf("no response from OpenAI")
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
		Msg("Successfully generated lyrics")

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
