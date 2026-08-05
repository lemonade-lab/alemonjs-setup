package robot

import "testing"

func TestSplitGitPackageSourceKeepsTag(t *testing.T) {
	repository, ref, err := splitGitPackageSource("git+https://github.com/lemonade-lab/example.git#v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if repository != "https://github.com/lemonade-lab/example.git" || ref != "v1.2.3" {
		t.Fatalf("got %q, %q", repository, ref)
	}
	if name := localPackageName("git+https://github.com/lemonade-lab/example.git#v1.2.3"); name != "example" {
		t.Fatalf("name = %q", name)
	}
	if _, _, err := splitGitPackageSource("git+https://github.com/lemonade-lab/example.git#--upload-pack=x"); err == nil {
		t.Fatal("unsafe ref must be rejected")
	}
}
