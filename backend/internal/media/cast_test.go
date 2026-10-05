package media

import "testing"

func TestCastIdentity(t *testing.T) {
	a := NewCastMember(" Same Name ", " Lead ", 42)
	if a.Name != "Same Name" || a.Character != "Lead" {
		t.Fatalf("credit was not trimmed: %+v", a)
	}
	if a.ID != NewCastMember("Translated Name", "Other role", 42).ID {
		t.Fatal("provider identity must survive name and role changes")
	}
	if a.ID == NewCastMember("Same Name", "Lead", 43).ID || a.ID == NewCastMember("Same Name", "Lead", 0).ID {
		t.Fatal("unrelated provider and local identities were conflated")
	}
	if NewCastMember("  SAME   Name ", "", 0).ID != NewCastMember("same name", "", 0).ID {
		t.Fatal("local names were not normalized")
	}
}
