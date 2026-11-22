package ui

import "testing"

func TestSaveDialogDefaultName(t *testing.T) {
	filters := []FileFilter{
		{DisplayName: "PNG Files (*.png)", Patterns: []string{"*.png"}},
		{DisplayName: "JPEG Files (*.jpg)", Patterns: []string{"*.jpg"}},
	}

	cases := []struct {
		name    string
		input   string
		filters []FileFilter
		want    string
	}{
		{
			name:    "bare name uses first filter extension",
			input:   "glare_removed_dead_on",
			filters: filters,
			want:    "glare_removed_dead_on.png",
		},
		{
			name:    "existing extension is preserved",
			input:   "glare_removed_dead_on.jpg",
			filters: filters,
			want:    "glare_removed_dead_on.jpg",
		},
		{
			name:    "wildcard-only filter leaves bare name unchanged",
			input:   "glare_removed_dead_on",
			filters: []FileFilter{{DisplayName: "All Files", Patterns: []string{"*.*"}}},
			want:    "glare_removed_dead_on",
		},
		{
			name:    "empty name stays empty",
			input:   "",
			filters: filters,
			want:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := saveDialogDefaultName(tc.input, tc.filters); got != tc.want {
				t.Fatalf("saveDialogDefaultName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
