package format

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    OutputFormat
		wantErr bool
	}{
		{name: "text lowercase", input: "text", want: Text, wantErr: false},
		{name: "json lowercase", input: "json", want: JSON, wantErr: false},
		{name: "text uppercase", input: "TEXT", want: Text, wantErr: false},
		{name: "json uppercase", input: "JSON", want: JSON, wantErr: false},
		{name: "text mixed case", input: "Text", want: Text, wantErr: false},
		{name: "json mixed case", input: "Json", want: JSON, wantErr: false},
		{name: "text with leading spaces", input: "  text", want: Text, wantErr: false},
		{name: "json with trailing spaces", input: "json  ", want: JSON, wantErr: false},
		{name: "text with surrounding spaces", input: "  text  ", want: Text, wantErr: false},
		{name: "empty string", input: "", want: "", wantErr: true},
		{name: "invalid format", input: "xml", want: "", wantErr: true},
		{name: "only whitespace", input: "   ", want: "", wantErr: true},
		{name: "numeric input", input: "123", want: "", wantErr: true},
		{name: "partial match", input: "tex", want: "", wantErr: true},
		{name: "json with extra chars", input: "jsonx", want: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "valid text", input: "text", want: true},
		{name: "valid json", input: "json", want: true},
		{name: "valid TEXT uppercase", input: "TEXT", want: true},
		{name: "valid JSON uppercase", input: "JSON", want: true},
		{name: "invalid format", input: "xml", want: false},
		{name: "empty string", input: "", want: false},
		{name: "whitespace only", input: "   ", want: false},
		{name: "partial match", input: "jso", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValid(tt.input)
			if got != tt.want {
				t.Errorf("IsValid(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatOutput(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		formatStr string
		wantJSON  bool // if true, validate JSON structure; otherwise expect raw content
	}{
		{name: "text format returns content as-is", content: "hello world", formatStr: "text", wantJSON: false},
		{name: "json format wraps in JSON", content: "hello world", formatStr: "json", wantJSON: true},
		{name: "invalid format defaults to text", content: "hello world", formatStr: "xml", wantJSON: false},
		{name: "empty format defaults to text", content: "hello world", formatStr: "", wantJSON: false},
		{name: "empty content text", content: "", formatStr: "text", wantJSON: false},
		{name: "empty content json", content: "", formatStr: "json", wantJSON: true},
		{name: "content with newlines text", content: "line1\nline2", formatStr: "text", wantJSON: false},
		{name: "content with newlines json", content: "line1\nline2", formatStr: "json", wantJSON: true},
		{name: "content with special chars json", content: "say \"hello\" \\ world", formatStr: "json", wantJSON: true},
		{name: "content with tabs json", content: "col1\tcol2", formatStr: "json", wantJSON: true},
		{name: "content with unicode json", content: "hello 世界 🌍", formatStr: "json", wantJSON: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatOutput(tt.content, tt.formatStr)

			if tt.wantJSON {
				// Verify it's valid JSON
				var parsed struct {
					Response string `json:"response"`
				}
				if err := json.Unmarshal([]byte(got), &parsed); err != nil {
					t.Fatalf("FormatOutput(%q, %q) produced invalid JSON: %v\nOutput: %s", tt.content, tt.formatStr, err, got)
				}
				// Verify the content is preserved
				if parsed.Response != tt.content {
					t.Errorf("FormatOutput(%q, %q) JSON response field = %q, want %q", tt.content, tt.formatStr, parsed.Response, tt.content)
				}
			} else {
				if got != tt.content {
					t.Errorf("FormatOutput(%q, %q) = %q, want %q", tt.content, tt.formatStr, got, tt.content)
				}
			}
		})
	}
}

func TestFormatAsJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "simple string", content: "hello"},
		{name: "empty string", content: ""},
		{name: "with quotes", content: `say "hello"`},
		{name: "with backslashes", content: `path\to\file`},
		{name: "with newlines", content: "line1\nline2\nline3"},
		{name: "with tabs", content: "col1\tcol2"},
		{name: "with carriage return", content: "line1\r\nline2"},
		{name: "with all special chars", content: "quotes: \" backslash: \\ newline: \n tab: \t"},
		{name: "with unicode", content: "café résumé naïve"},
		{name: "large content", content: strings.Repeat("a", 10000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAsJSON(tt.content)

			// Must be valid JSON
			var parsed struct {
				Response string `json:"response"`
			}
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Fatalf("formatAsJSON(%q) produced invalid JSON: %v\nOutput: %s", tt.content, err, got)
			}

			// Content must be preserved exactly
			if parsed.Response != tt.content {
				t.Errorf("formatAsJSON(%q) response = %q, want %q", tt.content, parsed.Response, tt.content)
			}

			// Should be indented (contain newlines in the JSON structure)
			if !strings.Contains(got, "\n") {
				t.Errorf("formatAsJSON(%q) expected indented JSON output but got: %s", tt.content, got)
			}
		})
	}
}

func TestOutputFormatString(t *testing.T) {
	tests := []struct {
		name   string
		format OutputFormat
		want   string
	}{
		{name: "text format", format: Text, want: "text"},
		{name: "json format", format: JSON, want: "json"},
		{name: "empty format", format: OutputFormat(""), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.format.String()
			if got != tt.want {
				t.Errorf("OutputFormat(%q).String() = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}
