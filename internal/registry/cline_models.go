package registry

// GetClineModels returns the fallback Cline model definitions.
func GetClineModels() []*ModelInfo {
	return []*ModelInfo{
		{
			ID:                  "cline/auto",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "cline",
			Type:                "cline",
			DisplayName:         "Cline Auto",
			Description:         "Automatic model selection by Cline",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			SupportedEndpoints:  []string{"/chat/completions"},
		},
		{
			ID:                  "claude-4-sonnet",
			Object:              "model",
			Created:             1732752000,
			OwnedBy:             "cline",
			Type:                "cline",
			DisplayName:         "Claude 4 Sonnet",
			Description:         "Claude 4 Sonnet via Cline",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			SupportedEndpoints:  []string{"/chat/completions"},
		},
	}
}
