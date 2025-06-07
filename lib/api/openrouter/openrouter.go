package openrouter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
)

type OpenRouterClient struct {
	APIKey      string
	Model       string
	Temperature float64
}

func NewClient(apiKey, model string, temperature float64) *OpenRouterClient {
	if model == "" {
		model = "meta-llama/llama-4-scout:free"
	}
	if temperature == 0 {
		temperature = 0.7
	}
	return &OpenRouterClient{APIKey: apiKey, Model: model, Temperature: temperature}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
}

type Choice struct {
	Message Message `json:"message"`
}

type Response struct {
	Choices []Choice `json:"choices"`
}

// cleanLLMResponse удаляет Markdown-форматирование из ответа OpenRouter
func CleanLLMResponse(response string) string {
	// Удаляем ```json и ```, а также лишние пробелы и переносы строк
	trimmed := strings.TrimSpace(response)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func (client *OpenRouterClient) SendChat(systemPrompt, userPrompt string, temperature ...float64) (string, error) {
	temp := client.Temperature
	if len(temperature) > 0 {
		temp = temperature[0]
	}

	messages := []Message{}
	if systemPrompt != "" {
		messages = append(messages, Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, Message{Role: "user", Content: userPrompt})

	requestBody := Request{Model: client.Model, Messages: messages, Temperature: temp}
	jsonRequestBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request: %v", err)
	}

	url := "https://openrouter.ai/api/v1/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonRequestBody))
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+client.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request error. Status: %d. Response: %s", resp.StatusCode, string(body))
	}

	var apiResponse Response
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		return "", fmt.Errorf("error parsing response: %v", err)
	}
	if len(apiResponse.Choices) == 0 {
		return "", fmt.Errorf("no response received from OpenRouter")
	}

	responseText := apiResponse.Choices[0].Message.Content
	if strings.HasPrefix(responseText, "\\boxed{") && strings.HasSuffix(responseText, "}") {
		responseText = responseText[len("\\boxed{") : len(responseText)-1]
	}
	return responseText, nil
}

type SystemPrompts map[string]string

func LoadSystemPrompts(filename string) (SystemPrompts, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading file %s: %v", filename, err)
	}
	var prompts SystemPrompts
	err = yaml.Unmarshal(data, &prompts)
	if err != nil {
		return nil, fmt.Errorf("error parsing YAML file: %v", err)
	}
	return prompts, nil
}
