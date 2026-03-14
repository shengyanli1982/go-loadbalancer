package types

import "testing"

func TestNewModelCapabilitySetAllows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		availability map[string]bool
		model        string
		want         bool
	}{
		{
			name:         "nil availability allows all",
			availability: nil,
			model:        "model-a",
			want:         true,
		},
		{
			name:         "empty availability allows all",
			availability: map[string]bool{},
			model:        "model-a",
			want:         true,
		},
		{
			name: "single model hit",
			availability: map[string]bool{
				"model-a": true,
			},
			model: "model-a",
			want:  true,
		},
		{
			name: "single model miss",
			availability: map[string]bool{
				"model-a": true,
			},
			model: "model-b",
			want:  false,
		},
		{
			name: "multiple models hit",
			availability: map[string]bool{
				"model-a": true,
				"model-b": true,
			},
			model: "model-b",
			want:  true,
		},
		{
			name: "multiple models miss",
			availability: map[string]bool{
				"model-a": true,
				"model-b": true,
			},
			model: "model-c",
			want:  false,
		},
		{
			name: "all false should reject all models",
			availability: map[string]bool{
				"model-a": false,
				"model-b": false,
			},
			model: "model-a",
			want:  false,
		},
		{
			name: "empty model always allowed",
			availability: map[string]bool{
				"model-a": false,
			},
			model: "",
			want:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			set := NewModelCapabilitySet(tc.availability)
			got := set.Allows(tc.model)
			if got != tc.want {
				t.Fatalf("Allows(%q)=%v, want %v", tc.model, got, tc.want)
			}
		})
	}
}
