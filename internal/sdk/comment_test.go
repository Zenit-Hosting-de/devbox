package sdk

import "testing"

func TestXMLAnchorSkipsComments(t *testing.T) {
	in := []byte("<settings>\n  <!--\n  </servers>\n  -->\n  <a/> <!-- x -->\n</settings>\n")
	xml, err := CommentStyle("xml", "settings-corp")
	if err != nil {
		t.Fatal(err)
	}
	if HasLine(in, "</servers>", xml) {
		t.Fatal("a line inside a comment must not count")
	}
	if !HasLine(in, "</settings>", xml) {
		t.Fatal("a line after a closed comment must count")
	}
	if _, err := SetBlockBefore(in, "n", []byte("x"), xml, "</servers>"); err == nil {
		t.Fatal("want an error: the only anchor is commented out")
	}
	if c, err := CommentStyle("", "settings-corp"); err != nil || c.End != "" {
		t.Fatalf("auto on a name without extension is hash: %v %v", c, err)
	}
	if _, err := CommentStyle("bogus", "x"); err == nil {
		t.Fatal("unknown style must be an error")
	}
}
