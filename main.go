package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
)

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

// LyricsService handles the lyrics generation logic
type LyricsService struct {
	openaiClient *openai.Client
}

// NewLyricsService creates a new lyrics service
func NewLyricsService(apiKey string) *LyricsService {
	client := openai.NewClient(apiKey)
	return &LyricsService{
		openaiClient: client,
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
		log.Println("No .env file found")
	}

	// Get OpenAI API key
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize services
	lyricsService := NewLyricsService(openaiKey)

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

	// Start server
	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
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
			log.Printf("Error generating lyrics: %v", err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error:   "generation_failed",
				Message: "Failed to generate lyrics. Please try again.",
			})
			return
		}

		c.JSON(http.StatusOK, response)
	}
}

// GenerateLyrics generates song lyrics using OpenAI
func (s *LyricsService) GenerateLyrics(ctx context.Context, req LyricsRequest) (*LyricsResponse, error) {
	// Create prompt
	prompt := s.buildPrompt(req)

	// Call OpenAI API
	response, err := s.openaiClient.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT3Dot5Turbo,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a professional songwriter who creates family-friendly, appropriate lyrics for all ages. Always ensure content is positive and suitable for children.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   1000,
			Temperature: 0.8,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	// Parse and structure the response
	generatedText := response.Choices[0].Message.Content
	lyrics := s.parseLyrics(generatedText, req)

	// Count words
	wordCount := s.countWords(generatedText)

	// Create response
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
