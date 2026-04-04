package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"toolbox-example/internal/creds"

	"github.com/spf13/cobra"
)

const (
	fishTTSURL      = "https://api.fish.audio/v1/tts"
	catboxUploadURL = "https://catbox.moe/user/api.php"
)

func voiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "voice",
		Short: "Text-to-speech via fish.audio + optional catbox upload",
	}
	cmd.AddCommand(voiceSayCmd())
	return cmd
}

func voiceSayCmd() *cobra.Command {
	var text, direction, voiceID, outFile, model, format string
	var speed, temp, topP float64
	var bitrate int
	var noCatbox, normalize bool

	cmd := &cobra.Command{
		Use:   "say",
		Short: "Generate speech and optionally upload to catbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := creds.Get("FISH_AUDIO_API_KEY")
			if err != nil {
				return fmt.Errorf("get FISH_AUDIO_API_KEY via goat creds: %w", err)
			}
			if voiceID == "" {
				voiceID, _ = creds.Get("FISH_AUDIO_VOICE_ID")
			}
			if voiceID == "" {
				return fmt.Errorf("no voice configured; set FISH_AUDIO_VOICE_ID with goat creds or pass --voice")
			}

			if text == "" {
				stat, _ := os.Stdin.Stat()
				if (stat.Mode() & os.ModeCharDevice) == 0 {
					b, err := io.ReadAll(os.Stdin)
					if err != nil {
						return fmt.Errorf("reading stdin: %w", err)
					}
					text = strings.TrimSpace(string(b))
				}
			}
			if text == "" {
				return fmt.Errorf("no text provided; use --text or pipe via stdin")
			}

			if direction != "" {
				text = "[" + direction + "] " + text
			}
			if outFile == "" {
				outFile = "/tmp/toolbox-voice-out." + format
			}

			body := map[string]any{
				"text":         text,
				"reference_id": voiceID,
				"format":       format,
				"mp3_bitrate":  bitrate,
				"latency":      "normal",
				"temperature":  temp,
				"top_p":        topP,
				"normalize":    normalize,
			}
			if speed != 1.0 {
				body["prosody"] = map[string]any{"speed": speed}
			}

			jsonBody, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("marshal request: %w", err)
			}

			req, err := http.NewRequest("POST", fishTTSURL, bytes.NewReader(jsonBody))
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("model", model)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("TTS request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errBody, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("TTS API returned %d: %s", resp.StatusCode, string(errBody))
			}

			audioData, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("reading audio response: %w", err)
			}
			if err := os.WriteFile(outFile, audioData, 0644); err != nil {
				return fmt.Errorf("writing file: %w", err)
			}

			if noCatbox {
				fmt.Println(outFile)
				return nil
			}

			catboxURL, err := uploadToCatbox(outFile)
			if err != nil {
				return fmt.Errorf("catbox upload failed: %w", err)
			}
			fmt.Println(catboxURL)
			return nil
		},
	}

	cmd.Flags().StringVar(&text, "text", "", "Text to speak (or pipe via stdin)")
	cmd.Flags().StringVar(&direction, "direction", "", "Global voice direction")
	cmd.Flags().StringVar(&voiceID, "voice", "", "Fish.audio voice model ID; falls back to FISH_AUDIO_VOICE_ID")
	cmd.Flags().StringVar(&outFile, "out", "", "Output file path")
	cmd.Flags().Float64Var(&speed, "speed", 1.0, "Speech speed via prosody")
	cmd.Flags().Float64Var(&temp, "temp", 0.7, "Temperature/expressiveness")
	cmd.Flags().Float64Var(&topP, "top-p", 0.7, "Nucleus sampling diversity")
	cmd.Flags().StringVar(&model, "model", "s2-pro", "TTS model")
	cmd.Flags().StringVar(&format, "format", "mp3", "Output format")
	cmd.Flags().IntVar(&bitrate, "bitrate", 128, "MP3 bitrate")
	cmd.Flags().BoolVar(&noCatbox, "no-catbox", false, "Skip catbox upload, just save locally")
	cmd.Flags().BoolVar(&normalize, "normalize", true, "Normalize text")
	return cmd
}

func uploadToCatbox(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("reqtype", "fileupload")
	part, err := w.CreateFormFile("fileToUpload", filePath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	_ = w.Close()

	resp, err := http.Post(catboxUploadURL, w.FormDataContentType(), &buf)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	url := strings.TrimSpace(string(body))
	if !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("unexpected catbox response: %s", url)
	}
	return url, nil
}
