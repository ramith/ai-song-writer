# ai-song-writer


An API that generates original song lyrics using AI (OpenAI) based on user-provided keywords, genre, emotion, and language preferences.

## 🎵 Features

- **Multi-genre Support**: Pop, Rock, Country, Hip-Hop, R&B, Jazz, Folk, Electronic, Classical, Reggae, Blues, Indie
- **Emotion-based Generation**: Happy, Sad, Romantic, Energetic, Melancholic, Hopeful, Nostalgic, Peaceful, Excited, Contemplative
- **Multi-language Support**: English, Spanish, French, German, Italian, Portuguese, Japanese, Korean
- **Customizable Structure**: Configure verses, chorus, and bridge
- **Family-friendly Content**: All generated lyrics are appropriate for all ages

## 🚀 Quick Start

### Prerequisites

- Go 1.21+
- OpenAI API Key

### Local Development

1. **Clone and setup**:
```bash
git clone <repository-url>
cd gen-ai-sample
```

2. **Install dependencies**:
```bash
go mod tidy
```

3. **Set environment variables**:
```bash
cp .env.example .env
# Edit .env and add your OpenAI API key
```

4. **Run the server**:
```bash
go run main.go
```

The API will be available at `http://localhost:8080`

### Choreo Deployment

1. **Build Docker image**:
```bash
docker build -t songlyrics-api .
```

2. **Deploy to Choreo**:
   - Push code to your Git repository
   - Connect repository to Choreo
   - Configure environment variables in Choreo dashboard
   - Deploy the service

## 📖 API Usage

### Generate Lyrics

**POST** `/api/v1/generate`

```json
{
  "keywords": ["love", "sunset", "journey"],
  "genre": "pop",
  "emotion": "romantic",
  "language": "english",
  "structure": {
    "verses": 2,
    "chorus": true,
    "bridge": true
  }
}
```

**Response**:
```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "lyrics": {
    "title": "Love at Sunset",
    "structure": {
      "verse1": "Walking down this winding road...",
      "chorus": "Love finds a way when the sunset glows...",
      "verse2": "Every journey has its story...",
      "bridge": "Through the valleys and the peaks..."
    }
  },
  "metadata": {
    "genre": "pop",
    "emotion": "romantic",
    "language": "english",
    "keywords_used": ["love", "sunset", "journey"],
    "created_at": "2025-08-26T10:30:00Z",
    "word_count": 156
  }
}
```

### Health Check

**GET** `/health`

```json
{
  "status": "healthy",
  "timestamp": "2025-08-26T10:30:00Z",
  "version": "1.0.0"
}
```

## 🎛️ Supported Options

### Genres
- pop, rock, country, hip-hop, r&b, jazz, folk, electronic, classical, reggae, blues, indie

### Emotions
- happy, sad, romantic, energetic, melancholic, hopeful, nostalgic, peaceful, excited, contemplative

### Languages
- english, spanish, french, german, italian, portuguese, japanese, korean

## 🔧 Configuration

### Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `OPENAI_API_KEY` | Your OpenAI API key | Yes | - |
| `PORT` | Server port | No | 8080 |

## 📝 Example Requests

### Simple Pop Song
```bash
curl -X POST http://localhost:8080/api/v1/generate \
  -H "Content-Type: application/json" \
  -d '{
    "keywords": ["summer", "freedom", "youth"],
    "genre": "pop",
    "emotion": "happy",
    "language": "english"
  }'
```

### Romantic Ballad
```bash
curl -X POST http://localhost:8080/api/v1/generate \
  -H "Content-Type: application/json" \
  -d '{
    "keywords": ["heart", "forever", "promise"],
    "genre": "r&b",
    "emotion": "romantic",
    "language": "english",
    "structure": {
      "verses": 3,
      "chorus": true,
      "bridge": true
    }
  }'
```

### Spanish Folk Song
```bash
curl -X POST http://localhost:8080/api/v1/generate \
  -H "Content-Type: application/json" \
  -d '{
    "keywords": ["casa", "familia", "recuerdos"],
    "genre": "folk",
    "emotion": "nostalgic",
    "language": "spanish"
  }'
```

## 🏗️ Architecture

- **Backend**: Go with Gin framework
- **AI Service**: OpenAI GPT-3.5-turbo
- **Deployment**: Docker + Choreo
- **Content Safety**: Built-in filtering for family-friendly content

## 📊 Rate Limits

- 10 requests per minute per client
- Burst allowance of 20 requests
- Response time: < 5 seconds

## 🛡️ Content Safety

All generated lyrics are:
- Family-friendly and appropriate for all ages
- Free from explicit language, violence, or inappropriate themes
- Positive and suitable for children
- Filtered through OpenAI's content moderation

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## � Team


- **AI (OpenAI) Expert**: Responsible for integrating and optimizing AI/ML models, especially OpenAI APIs, for song writing features.
- **Go Expert**: Designs and implements robust, idiomatic Go code for backend and core logic.
- **API Designer**: Crafts clear, scalable, and well-documented APIs for internal and external use.
- **Product Manager**: Defines product vision, prioritizes features, and ensures alignment with user needs and business goals.
- **Business Analyst**: Gathers requirements, analyzes market trends, and translates business needs into actionable technical tasks.
- **QA Engineer**: Conducts comprehensive testing including API testing, quality assurance, test automation, and bug reporting to ensure product reliability.

*If you are interested in contributing as one of these roles, please open an issue or pull request!*

## �📄 License

This project is licensed under the Apache License, Version 2.0. See the LICENSE file for details.

## 🆘 Support

For support and questions:
- Create an issue in the repository
- Contact: support@songlyrics.api

---

**Version**: 1.0.0  
**Last Updated**: August 26, 2025
