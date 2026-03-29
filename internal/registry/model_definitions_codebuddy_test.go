package registry

import "testing"

func TestGetCodeBuddyModelsMatchesRequestedCLISubset(t *testing.T) {
	models := GetCodeBuddyModels()
	if len(models) == 0 {
		t.Fatal("expected CodeBuddy models")
	}

	byID := make(map[string]*ModelInfo, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		byID[model.ID] = model
	}

	present := []string{
		"codebuddy/glm-5.0",
		"codebuddy/kimi-k2.5",
		"codebuddy/deepseek-v3-2-volc",
		"codebuddy/default-model-lite",
		"codebuddy/gemini-3.0-pro-image",
		"codebuddy/gemini-3.1-flash-image",
		"codebuddy/gemini-2.5-flash-image",
		"codebuddy/hunyuan-image-v3.0",
		"codebuddy/hunyuan-image-v2.0-general-edit",
		"codebuddy/gpt-5.4",
		"codebuddy/gemini-3.1-pro",
		"codebuddy/gpt-5.3-codex",
	}
	for _, id := range present {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected CodeBuddy model %q to be present", id)
		}
	}

	absent := []string{"glm-4.7", "minimax-m2.5", "hunyuan-2.0-thinking"}
	for _, id := range absent {
		if _, ok := byID[id]; ok {
			t.Fatalf("expected CodeBuddy model %q to be removed", id)
		}
	}

	if got := byID["codebuddy/gpt-5.4"].DisplayName; got != "GPT-5.4" {
		t.Fatalf("expected gpt-5.4 display name GPT-5.4, got %q", got)
	}
	if got := byID["codebuddy/gemini-3.1-pro"].DisplayName; got != "Gemini 3.1 Pro" {
		t.Fatalf("expected gemini-3.1-pro display name Geminin 3.1 Pro, got %q", got)
	}
	imageModel := byID["codebuddy/gemini-3.0-pro-image"]
	if len(imageModel.SupportedOutputModalities) == 0 || imageModel.SupportedOutputModalities[0] != "IMAGE" {
		t.Fatalf("expected image model to advertise IMAGE output, got %+v", imageModel.SupportedOutputModalities)
	}
}
