package colour

import "testing"

func TestFamily(t *testing.T) {
	for rgba, want := range map[string]string{
		"#000000ff": "black", "#3d3e3d": "grey", "#ffffff": "white", "#ff8000ff": "orange", "#f55928ff": "orange",
		"#7a4a1c": "brown", "#ff0000": "red", "#00ff00": "green", "#0000ff": "blue", "#8000ff": "purple",
		"#800080": "purple", "#ff69b4": "pink", "#ffd700": "yellow", "#ffffff40": "natural", "#FF8000": "orange",
		"nope": "", "#fff": "", "": "",
	} {
		if got := Family(rgba); got != want {
			t.Errorf("Family(%q) = %q, want %q", rgba, got, want)
		}
	}
}

func TestIsFamily(t *testing.T) {
	if !IsFamily("orange") || IsFamily("#ff8000") || IsFamily("") {
		t.Fatal("IsFamily")
	}
}
