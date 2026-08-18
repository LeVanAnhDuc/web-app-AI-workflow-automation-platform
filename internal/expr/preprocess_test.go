package expr

import "testing"

func TestPreprocessRewritesOnlyOutsideQuotes(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"bare name", "$json.email", "_dollar_json.email"},
		{"several names", "$json.a + $itemIndex", "_dollar_json.a + _dollar_itemIndex"},
		{"node lookup", `$node["Fetch"].json`, `_dollar_node["Fetch"].json`},
		{"dollar amount in a double-quoted string", `"total: $5"`, `"total: $5"`},
		{"name inside a double-quoted string", `"$json"`, `"$json"`},
		{"name inside a single-quoted string", `'$json'`, `'$json'`},
		{"name inside a backtick string", "`$json`", "`$json`"},
		{"quoted left untouched, bare rewritten", `"$json" + $json.a`, `"$json" + _dollar_json.a`},
		{"escaped quote does not end the string", `"a\"$json" + $json`, `"a\"$json" + _dollar_json`},
		{"backslash is literal inside backticks", "`a\\` + $json", "`a\\` + _dollar_json"},
		{"lone dollar", "1 + $ 2", "1 + $ 2"},
		{"underscore starts an identifier", "$_x", "_dollar__x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustEqual(t, Preprocess(tc.src), tc.want, tc.src)
		})
	}
}

func TestPreprocessedStringLiteralsSurviveEvaluation(t *testing.T) {
	// The end-to-end proof of the scanner: the literals keep their $ while the
	// bare name in the same expression is still resolved.
	got, err := Evaluate(`{{ "total: $5 for " + $json.name + ' ($json)' }}`, testEnv())
	mustNoError(t, err, "evaluate")
	mustEqual(t, got, "total: $5 for Ada ($json)", "quoted dollars survive")
}

func TestSplitSegments(t *testing.T) {
	segs, err := split("a {{ 1 }} b {{ 2 }}")
	mustNoError(t, err, "split")
	mustEqual(t, segs, []segment{
		{text: "a "},
		{text: " 1 ", isExpr: true},
		{text: " b "},
		{text: " 2 ", isExpr: true},
	}, "segments")
}

func TestSplitToleratesBracesInsideTheExpression(t *testing.T) {
	segs, err := split(`{{ {"a": {"b": 1}} }}`)
	mustNoError(t, err, "split")
	mustEqual(t, segs, []segment{{text: ` {"a": {"b": 1}} `, isExpr: true}}, "one segment")
}

func TestSplitUnclosed(t *testing.T) {
	_, err := split("a {{ 1 ")
	mustError(t, err, "unclosed template")
}
