package audio

import (
	"context"
	"fmt"
	"log"

	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"github.com/gorilla/websocket"
	"google.golang.org/api/option"
	texttospeechpb "google.golang.org/genproto/googleapis/cloud/texttospeech/v1"
)

const (
	googleSearchURL = "https://www.googleapis.com/customsearch/v1"
)

// Audio conversion
func HandleUserInteraction(sessionID string, userInput string, wsConn *websocket.Conn) error {
	var immediateResponse string
	var err error

	immediateResponse, err = GenerateResponse(userInput)
	if err != nil {
		log.Println("Error generating response:", err)
	}

	audioData, err := ConvertTextToSpeech(immediateResponse)
	if err != nil {
		log.Println("Error converting text to speech:", err)
		return err
	}

	err = StreamAudio(wsConn, audioData)
	if err != nil {
		log.Println("Error streaming audio:", err)
		return err
	}
	return nil
}

func ConvertTextToSpeech(text string) ([]byte, error) {
	ctx := context.Background()
	client, err := texttospeech.NewClient(ctx, option.WithAPIKey(GoogleCloudAPIKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create TTS client: %w", err)
	}
	defer client.Close()

	req := &texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{Text: text},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: "en-US",
			SsmlGender:   texttospeechpb.SsmlVoiceGender_NEUTRAL,
		},
		AudioConfig: &texttospeechpb.AudioConfig{
			AudioEncoding: texttospeechpb.AudioEncoding_OGG_OPUS,
		},
	}

	resp, err := client.SynthesizeSpeech(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to synthesize speech: %w", err)
	}

	return resp.AudioContent, nil
}

func StreamAudio(conn *websocket.Conn, audioData []byte) error {
	// binary message
	err := conn.WriteMessage(websocket.BinaryMessage, audioData)
	if err != nil {
		return fmt.Errorf("failed to send audio data: %w", err)
	}
	return nil
}
