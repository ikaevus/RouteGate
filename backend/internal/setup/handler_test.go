package setup

import "testing"

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{name: "minimum", password: "routegate-12", valid: true},
		{name: "long", password: "correct horse battery staple", valid: true},
		{name: "short", password: "short", valid: false},
		{name: "whitespace", password: "            ", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			valid := validatePassword(test.password) == ""
			if valid != test.valid {
				t.Fatalf("validatePassword(%q) valid=%v, want %v", test.password, valid, test.valid)
			}
		})
	}
}

func TestRandomTokenShape(t *testing.T) {
	first, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("randomToken returned duplicate values")
	}
	if !validTokenShape(first) || !validTokenShape(second) {
		t.Fatal("randomToken returned an invalid token shape")
	}
}
