// Copyright (C) 2022 Michael J. Fromberger. All Rights Reserved.

package tomledit_test

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/transform"
	"github.com/google/go-cmp/cmp"
)

var (
	doEmit = flag.Bool("emit", false, "Emit formatted output to stdout")
)

func mustParse(t *testing.T, s string) *tomledit.Document {
	t.Helper()
	doc, err := tomledit.Parse(strings.NewReader(s))
	if err != nil {
		t.Logf("Input:\n%s", s)
		t.Fatalf("Parse failed: %v", err)
	}
	return doc
}

func mustFormat(t *testing.T, doc *tomledit.Document, more ...string) {
	t.Helper()

	if *doEmit {
		t.Logf("Writing formatted output for %s %s", t.Name(), strings.Join(more, " "))
		if err := tomledit.Format(os.Stdout, doc); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
	}
}

const testDoc11 = `
# Multi-line inline table with trailing comma and comments
point = { # comment 1
    # comment 2
    x = 1,
    # comment 3
    y = 2, # comment 4
} # comment 5

# Escape sequences
hex = "Jos\xE9"
esc = "\e[31mred\e[0m"

[settings]
config = {
    timeout = 30,
    retries = 3,
}
`

const testDoc = `
# free 1 line 1
# free 1 line 2

# free 2

# top-level mapping
p = { q = [], r = {}}
# bound
[ first . table ]
  # Various spacing shenanigans.
  a = { b = 3, c = [4,5, # ppp
        6,
  ] } # qqq
  # Compound keys and values.
  fuss . budget = {x = true} # barbaric yawp
  x = 14  # hey what's up
  y = 'three'
# A complex value.
z = [4, 5, # whatever
    ['a', 'b', # hark
          'c' # hey 
        , 'd'], # foob
      6, 7] #hate
list = [
10, 20, 30
, 40,
]
[second-table]
foo = 'bar'
# A repeated table array.
[[p]]
q = 1
r = { s.t = 'u'} # v
[[p]]
q = 2
# free comment again
`

func TestFormat(t *testing.T) {
	mustFormat(t, mustParse(t, testDoc))

	t.Run("Comments", func(t *testing.T) {
		// Check that synthesized comments are properly processed to handle
		// internal newlines and comment markers.
		doc := &tomledit.Document{
			Global: &tomledit.Section{
				Items: []parser.Item{
					parser.Comments{`
                   # This is a comment
                   #    that spans multiple lines
                   as you can see.
                  `,
						"#\n# end",
					},
				},
			},
		}
		const want = "# This is a comment\n" + // a regular line
			"#    that spans multiple lines\n" + // preserve indentation with "#"
			"# as you can see.\n" + // add "#" if it is missing
			"#\n" + // handle internal line breaks
			"# end\n"
		var buf bytes.Buffer
		if err := tomledit.Format(&buf, doc); err != nil {
			t.Fatalf("Formatting failed: %v", err)
		}
		if diff := cmp.Diff(want, buf.String()); diff != "" {
			t.Errorf("Formatted output: (-want, +got)\n%s", diff)
		}
	})
}

func TestScan(t *testing.T) {
	doc := mustParse(t, testDoc)

	// Verify that free comments are kept free and correctly blocked.
	t.Run("Comments", func(t *testing.T) {
		var got []string
		for _, item := range doc.Global.Items {
			if c, ok := item.(parser.Comments); ok {
				got = append(got, c.String())
			}
		}
		for _, s := range doc.Sections {
			for _, item := range s.Items {
				if c, ok := item.(parser.Comments); ok {
					got = append(got, c.String())
				}
			}
		}
		want := []string{"# free 1 line 1\n# free 1 line 2", "# free 2", "# free comment again"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Wrong comments: (-want, +got)\n%s", diff)
		}
	})

	// Check that the expected key-value mappings are captured, in order.
	t.Run("KeyValues", func(t *testing.T) {
		var keys []string
		doc.Scan(func(key parser.Key, elt *tomledit.Entry) bool {
			keys = append(keys, key.String())
			return true
		})

		// All the keys defined in the test table, in definition order.  This
		// must be updated if the test input changes.
		want := []string{
			// Top-level mappings.
			"p", "p.q", "p.r",

			// Standard table sections.
			"first.table", "first.table.a", "first.table.a.b", "first.table.a.c",
			"first.table.fuss.budget", "first.table.fuss.budget.x",
			"first.table.x", "first.table.y", "first.table.z", "first.table.list",

			"second-table", "second-table.foo",

			// Array tables.
			"p", "p.q", "p.r", "p.r.s.t", // first array element
			"p", "p.q", // second array element
		}
		if diff := cmp.Diff(want, keys); diff != "" {
			t.Errorf("Scan reported the wrong keys: (-want, +got)\n%s", diff)
		}
	})

	t.Run("Find", func(t *testing.T) {
		const wantMatches = 3

		found := doc.Find("p")
		if len(found) != wantMatches {
			t.Errorf("Find: got %d matches, want %d", len(found), wantMatches)
		}
		t.Logf("Matches: %v", found)
	})

	t.Run("FindGlobal", func(t *testing.T) {
		found := transform.FindTable(doc)
		if found == nil {
			t.Error("Global table not found")
		} else if found.Section != doc.Global {
			t.Errorf("Global table: got %v, want %v", found, doc.Global)
		}
	})

	t.Run("FindRemovable", func(t *testing.T) {
		tab := transform.FindTable(doc, "second-table")
		if tab == nil {
			t.Error("Table not found")
		} else if !tab.Remove() {
			t.Error("Table not removed")
		} else if check := transform.FindTable(doc, "second-table"); check != nil {
			t.Errorf("Removal did not work: found %v", check)
		}
	})
}

func TestEdit(t *testing.T) {
	tests := []struct {
		desc, input string
		want        string
		edit        func(*tomledit.Document)
	}{
		{
			desc:  "replace global",
			input: "key = {'x.y' = 0}",
			want:  "key = []",
			edit: func(doc *tomledit.Document) {
				doc.First("key").Value.X = parser.Array(nil)
			},
		},
		{
			desc:  "replace inline",
			input: "key={x=true}",
			want:  "key = {x = [1]}",
			edit: func(doc *tomledit.Document) {
				doc.First("key", "x").Value = parser.MustValue(`[1]`)
			},
		},
		{
			desc:  "remove global",
			input: "x=5\ny=10\n[z]\nok=true",
			want:  "[z]\nok = true",
			edit: func(doc *tomledit.Document) {
				doc.Global = nil
			},
		},
		{
			desc:  "remove inline",
			input: "[top]\nx={a=1,c=2}\n",
			want:  "[top]\nx = {a = 1}",
			edit: func(doc *tomledit.Document) {
				doc.First("top", "x", "c").Remove()
			},
		},
		{
			desc:  "remove section",
			input: "# A\n[a]\na=true\n\n# B\n[b]\nb=false\n[c]\nc=true",
			want:  "# A\n[a]\na = true\n\n[c]\nc = true",
			edit: func(doc *tomledit.Document) {
				doc.First("b").Remove()
			},
		},
		{
			desc:  "permute sections",
			input: "# A\n[a]\na=true\n\n# B\n[b]\nb=true\n",
			want:  "# B\n[b]\nb = true\n\n# A\n[a]\na = true",
			edit: func(doc *tomledit.Document) {
				doc.Sections = append(doc.Sections[1:], doc.Sections[0])
			},
		},
		{
			desc:  "insert global mapping",
			input: "x=0",
			want:  "x = 0\ny = 19  # OK",
			edit: func(doc *tomledit.Document) {
				doc.Global.Items = append(doc.Global.Items, &parser.KeyValue{
					Name:  parser.Key{"y"},
					Value: parser.MustValue(`19 # OK`),
				})
			},
		},
		{
			desc:  "insert table mapping",
			input: "[x]\ny=5",
			want:  "[x]\ny = 5\nz = [36, 24, 36]  # only if she's 5'3\"",
			edit: func(doc *tomledit.Document) {
				tab := doc.Sections[0]
				tab.Items = append(tab.Items, &parser.KeyValue{
					Name:  parser.Key{"z"},
					Value: parser.MustValue(`[36,24,36]# only if she's 5'3"`),
				})
			},
		},
		{
			desc:  "insert inline mapping",
			input: "x={a=0}",
			want:  "x = {a = 0, b = 'apples'}",
			edit: func(doc *tomledit.Document) {
				kv := doc.First("x").KeyValue
				tab := kv.Value.X.(parser.Inline)
				tab.Items = append(tab.Items, &parser.KeyValue{
					Name:  parser.Key{"b"},
					Value: parser.MustValue(`'apples'`),
				})
				kv.Value.X = tab
			},
		},
		{
			desc:  "sort key-value items",
			input: "# stay1\n\n# xc\nx=5\n# stay2\n\na=1\nm=3\n# rc\nr=4\na=2",
			want:  "# stay1\n\na = 1\n\n# stay2\n\na = 2\nm = 3\n\n# rc\nr = 4\n\n# xc\nx = 5",
			edit: func(doc *tomledit.Document) {
				transform.SortKeyValuesByName(doc.Global.Items)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			doc := mustParse(t, test.input)
			test.edit(doc)

			var buf bytes.Buffer
			if err := tomledit.Format(&buf, doc); err != nil {
				t.Fatalf("Format: %v", err)
			}
			got := strings.TrimSpace(buf.String())
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Wrong output: (-want, +got)\n%s", diff)
			}
		})
	}
}

func TestFormat11(t *testing.T) {
	mustFormat(t, mustParse(t, testDoc11))

	t.Run("MultilineInline", func(t *testing.T) {
		doc := &tomledit.Document{
			Global: &tomledit.Section{
				Items: []parser.Item{
					&parser.KeyValue{
						Name: parser.Key{"x"},
						Value: parser.Value{
							X: parser.Inline{
								Items: []*parser.KeyValue{
									{
										Name:  parser.Key{"a"},
										Value: parser.MustValue("1"),
										Line:  2,
									},
									{
										Name:  parser.Key{"b"},
										Value: parser.MustValue("2"),
										Line:  3,
									},
								},
							},
							Line: 1,
						},
					},
				},
			},
		}
		const want = "x = {\n" +
			"    a = 1,\n" +
			"    b = 2,\n" +
			"}\n"
		var buf bytes.Buffer
		if err := tomledit.Format(&buf, doc); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
		if diff := cmp.Diff(want, buf.String()); diff != "" {
			t.Errorf("Formatted output: (-want, +got)\n%s", diff)
		}
	})

	t.Run("MultilineInlineComments", func(t *testing.T) {
		doc := &tomledit.Document{
			Global: &tomledit.Section{
				Items: []parser.Item{
					&parser.KeyValue{
						Name: parser.Key{"point"},
						Value: parser.Value{
							X: parser.Inline{
								Trailer: "# comment 1",
								Items: []*parser.KeyValue{
									{
										Block: parser.Comments{"# comment 2"},
										Name:  parser.Key{"x"},
										Value: parser.MustValue("1"),
										Line:  3,
									},
									{
										Block: parser.Comments{"# comment 3"},
										Name:  parser.Key{"y"},
										Value: parser.MustValue("2").WithComment("# comment 4"),
										Line:  5,
									},
								},
							},
							Trailer: "# comment 5",
							Line:    1,
						},
					},
				},
			},
		}
		const want = "point = {  # comment 1\n" +
			"    # comment 2\n" +
			"    x = 1,\n" +
			"    # comment 3\n" +
			"    y = 2,  # comment 4\n" +
			"}  # comment 5\n"
		var buf bytes.Buffer
		if err := tomledit.Format(&buf, doc); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
		if diff := cmp.Diff(want, buf.String()); diff != "" {
			t.Errorf("Formatted output: (-want, +got)\n%s", diff)
		}
	})

	t.Run("EscapeSequences", func(t *testing.T) {
		doc := &tomledit.Document{
			Global: &tomledit.Section{
				Items: []parser.Item{
					&parser.KeyValue{
						Name:  parser.Key{"hex"},
						Value: parser.MustValue(`"Jos\xE9"`),
					},
					&parser.KeyValue{
						Name:  parser.Key{"esc"},
						Value: parser.MustValue(`"\e[0m"`),
					},
				},
			},
		}
		const want = `hex = "Jos\xE9"` + "\n" +
			`esc = "\e[0m"` + "\n"
		var buf bytes.Buffer
		if err := tomledit.Format(&buf, doc); err != nil {
			t.Fatalf("Format failed: %v", err)
		}
		if diff := cmp.Diff(want, buf.String()); diff != "" {
			t.Errorf("Formatted output: (-want, +got)\n%s", diff)
		}
	})
}

func TestScan11(t *testing.T) {
	doc := mustParse(t, testDoc11)

	t.Run("KeyValues", func(t *testing.T) {
		var keys []string
		doc.Scan(func(key parser.Key, elt *tomledit.Entry) bool {
			keys = append(keys, key.String())
			return true
		})

		want := []string{
			// Global mappings.
			"point", "point.x", "point.y",
			"hex", "esc",

			// [settings] section.
			"settings", "settings.config", "settings.config.timeout", "settings.config.retries",
		}
		if diff := cmp.Diff(want, keys); diff != "" {
			t.Errorf("Scan reported the wrong keys: (-want, +got)\n%s", diff)
		}
	})

	t.Run("Find", func(t *testing.T) {
		// Keys inside multi-line inline tables should be findable.
		for _, key := range [][]string{
			{"point", "x"},
			{"point", "y"},
			{"settings", "config", "timeout"},
			{"settings", "config", "retries"},
		} {
			if e := doc.First(key...); e == nil {
				t.Errorf("First(%v): not found", key)
			}
		}
	})
}

func TestEdit11(t *testing.T) {
	tests := []struct {
		desc, input string
		want        string
		edit        func(*tomledit.Document)
	}{
		{
			desc:  "replace in multi-line inline table",
			input: "x = {\n    a = 1,\n    b = 2,\n}",
			want:  "x = {\n    a = 99,\n    b = 2,\n}",
			edit: func(doc *tomledit.Document) {
				doc.First("x", "a").Value = parser.MustValue("99")
			},
		},
		{
			desc:  "remove trailing comma multi-line inline tables",
			input: "x = {a = 1, b = 2,}",
			want:  "x = {a = 1}",
			edit: func(doc *tomledit.Document) {
				doc.First("x", "b").Remove()
			},
		},
		{
			desc:  "add to multi-line inline table",
			input: "x = {\n    a = 1,\n}",
			want:  "x = {\n    a = 1, c = 3,\n}",
			edit: func(doc *tomledit.Document) {
				kv := doc.First("x").KeyValue
				tab := kv.Value.X.(parser.Inline)
				tab.Items = append(tab.Items, &parser.KeyValue{
					Name:  parser.Key{"c"},
					Value: parser.MustValue("3"),
				})
				kv.Value.X = tab
			},
		},
		{
			desc:  "edit comments in multi-line inline table",
			input: "point = { # comment 1\n    # comment 2\n    x = 1,\n    # comment 3\n    y = 2, # comment 4\n} # comment 5",
			want:  "point = {  # comment 11\n    # comment 12\n    x = 1,\n    # comment 13\n    y = 2,  # comment 14\n}  # comment 15",
			edit: func(doc *tomledit.Document) {
				kv := doc.First("point").KeyValue
				tab := kv.Value.X.(parser.Inline)
				// # comment 1 is now the Trailer on the Inline.
				tab.Trailer = "# comment 11"
				kv.Value.X = tab
				// # comment 2 is in x's Block.
				tab.Items[0].Block = parser.Comments{"# comment 12"}
				// # comment 3 is in y's Block.
				tab.Items[1].Block = parser.Comments{"# comment 13"}
				// # comment 4 is y's trailing comment.
				tab.Items[1].Value.Trailer = "# comment 14"
				// # comment 5 is the outer value's trailing comment.
				kv.Value.Trailer = "# comment 15"
			},
		},
		{
			desc:  "replace hex escape value",
			input: "key = \"\\xE9\"",
			want:  "key = \"replaced\"",
			edit: func(doc *tomledit.Document) {
				doc.First("key").Value = parser.MustValue(`"replaced"`)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			doc := mustParse(t, test.input)
			test.edit(doc)

			var buf bytes.Buffer
			if err := tomledit.Format(&buf, doc); err != nil {
				t.Fatalf("Format: %v", err)
			}
			got := strings.TrimSpace(buf.String())
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Wrong output: (-want, +got)\n%s", diff)
			}
		})
	}
}
