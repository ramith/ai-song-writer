package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHealthCheck(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.GET("/health", healthCheck)

	// Test
	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	var response HealthResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "healthy", response.Status)
	assert.Equal(t, "1.0.0", response.Version)
}

func TestValidateGenre(t *testing.T) {
	tests := []struct {
		name     string
		genre    string
		expected bool
	}{
		{"Valid genre - pop", "pop", true},
		{"Valid genre - rock", "rock", true},
		{"Invalid genre", "unknown", false},
		{"Case insensitive", "POP", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidGenres[strings.ToLower(tt.genre)]
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateEmotion(t *testing.T) {
	tests := []struct {
		name     string
		emotion  string
		expected bool
	}{
		{"Valid emotion - happy", "happy", true},
		{"Valid emotion - sad", "sad", true},
		{"Invalid emotion", "angry", false},
		{"Case insensitive", "HAPPY", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidEmotions[strings.ToLower(tt.emotion)]
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateLyricsValidation(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.Default()

	// Mock service
	mockService := &LyricsService{}
	router.POST("/generate", generateLyrics(mockService))

	tests := []struct {
		name           string
		requestBody    LyricsRequest
		expectedStatus int
	}{
		{
			name: "Valid request",
			requestBody: LyricsRequest{
				Keywords: []string{"love", "sunset"},
				Genre:    "pop",
				Emotion:  "happy",
				Language: "english",
				Structure: SongStructure{
					Verses: 2,
					Chorus: true,
					Bridge: false,
				},
			},
			expectedStatus: http.StatusInternalServerError, // Will fail due to no OpenAI key, but validation passes
		},
		{
			name: "Invalid genre",
			requestBody: LyricsRequest{
				Keywords: []string{"love", "sunset"},
				Genre:    "invalid",
				Emotion:  "happy",
				Language: "english",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Invalid emotion",
			requestBody: LyricsRequest{
				Keywords: []string{"love", "sunset"},
				Genre:    "pop",
				Emotion:  "invalid",
				Language: "english",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Invalid language",
			requestBody: LyricsRequest{
				Keywords: []string{"love", "sunset"},
				Genre:    "pop",
				Emotion:  "happy",
				Language: "invalid",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "No keywords",
			requestBody: LyricsRequest{
				Keywords: []string{},
				Genre:    "pop",
				Emotion:  "happy",
				Language: "english",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.requestBody)
			req, _ := http.NewRequest("POST", "/generate", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestParseLyrics(t *testing.T) {
	service := &LyricsService{}

	testText := `[Title: Love Song]
[Verse 1]
This is the first verse
With multiple lines
[Chorus]
This is the chorus
Everyone sings along
[Verse 2]
This is the second verse
More lyrics here`

	req := LyricsRequest{
		Keywords: []string{"love"},
		Genre:    "pop",
		Emotion:  "happy",
		Language: "english",
	}

	result := service.parseLyrics(testText, req)

	assert.Equal(t, "Love Song", result.Title)
	assert.Contains(t, result.Structure, "verse 1")
	assert.Contains(t, result.Structure, "chorus")
	assert.Contains(t, result.Structure, "verse 2")
}

func TestCountWords(t *testing.T) {
	service := &LyricsService{}

	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"Simple text", "hello world", 2},
		{"Empty text", "", 0},
		{"Multi-line text", "hello\nworld\ntest", 3},
		{"Text with punctuation", "hello, world! how are you?", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.countWords(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}
