package files

import "testing"

func TestCleanDisplayNameRejectsTraversal(t *testing.T) {
	tests := []string{
		"",
		"../secret.txt",
		"..\\secret.txt",
		"nested/secret.txt",
		`nested\secret.txt`,
		"almost..secret.txt",
		"C:\\secret.txt",
	}

	for _, test := range tests {
		if _, err := CleanDisplayName(test); err == nil {
			t.Fatalf("CleanDisplayName(%q) allowed invalid name", test)
		}
	}
}

func TestCleanDisplayNameAllowsPlainName(t *testing.T) {
	name, err := CleanDisplayName("photo.jpg")
	if err != nil {
		t.Fatalf("CleanDisplayName returned error: %v", err)
	}
	if name != "photo.jpg" {
		t.Fatalf("CleanDisplayName = %q", name)
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	tests := []string{
		"",
		"../secret",
		"..\\secret",
		"nested/secret",
		`nested\secret`,
		"almost..secret",
	}

	for _, test := range tests {
		if _, err := SafeJoin(root, test); err == nil {
			t.Fatalf("SafeJoin(%q) allowed invalid storage name", test)
		}
	}
}
