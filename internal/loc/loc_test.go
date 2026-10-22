package loc

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/internal/files"
)

const (
	english    = "english"
	oneMessage = " a:0 \"x\"\n"
)

// TestParseFile checks the messages, their spans and the one malformed line of a file with a header, a comment, a
// blank line, versions and a text that holds quotes.
func TestParseFile(t *testing.T) {
	text := "l_english:\n# a comment\n\n key_a:0 \"Hello\"\n key_b: \"No version\"\n key_c:12 \"He said \"hi\" twice\"\nnot a message\n"
	file := Parse(text, "localization/english/x.yml")
	if file.Language != english {
		t.Errorf("language = %q, want english", file.Language)
	}
	if len(file.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(file.Messages))
	}
	want := []Message{
		{Key: "key_a", Text: "Hello", Version: 0},
		{Key: "key_b", Text: "No version", Version: 0},
		{Key: "key_c", Text: `He said "hi" twice`, Version: 12},
	}
	for i, message := range file.Messages {
		if message.Key != want[i].Key || message.Text != want[i].Text || message.Version != want[i].Version {
			t.Errorf("message %d = %+v, want %+v", i, message, want[i])
		}
		if text[message.KeySpan.Start:message.KeySpan.End] != message.Key {
			t.Errorf("message %d key span covers %q", i, text[message.KeySpan.Start:message.KeySpan.End])
		}
		if text[message.TextSpan.Start:message.TextSpan.End] != message.Text {
			t.Errorf("message %d text span covers %q", i, text[message.TextSpan.Start:message.TextSpan.End])
		}
		if !strings.HasPrefix(text[message.Span.Start:message.Span.End], message.Key) {
			t.Errorf("message %d span covers %q", i, text[message.Span.Start:message.Span.End])
		}
	}
	if len(file.Errors) != 1 || file.Errors[0].Code != "malformed-line" {
		t.Fatalf("errors = %+v, want one malformed-line", file.Errors)
	}
	if got := text[file.Errors[0].Span.Start:file.Errors[0].Span.End]; got != "not a message" {
		t.Errorf("error covers %q", got)
	}
}

// TestParseLanguageRule checks each source of a file's language and their order: the header, then the file name,
// then the folder, with a replace folder on either side.
func TestParseLanguageRule(t *testing.T) {
	tests := []struct {
		name string
		text string
		path string
		want string
	}{
		{"header", "l_French:\n" + oneMessage, "localization/english/a_l_german.yml", "french"},
		{"file name before folder", oneMessage, "localization/english/a_l_german.yml", "german"},
		{"file name", oneMessage, "mod/localization/a_l_German.yml", "german"},
		{"file name needs yml", oneMessage, "localization/english/a_l_german.txt", english},
		{"folder", oneMessage, "mod/localization/braz_por/a.yml", "braz_por"},
		{"folder then replace", oneMessage, "localization/english/replace/x.yml", english},
		{"replace then folder", oneMessage, "localization/replace/english/x.yml", english},
		{"localisation spelling", oneMessage, "mod/localisation/Spanish/x.yml", "spanish"},
		{"nothing says", oneMessage, "localization/x.yml", ""},
		{"not in a localization folder", oneMessage, "events/english/x.yml", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Parse(test.text, test.path).Language; got != test.want {
				t.Errorf("language = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSegmentsMarkup checks every markup form on literal texts, nested formats, markers that open nothing, and that
// the spans are in the file offsets and in order.
func TestSegmentsMarkup(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []Segment
	}{
		{"plain", "just text", []Segment{{Markup: MarkupText, Text: "just text"}}},
		{"key", "a $KEY$ b", []Segment{{Markup: MarkupText, Text: "a "}, {Markup: MarkupKey, Text: "KEY"}, {Markup: MarkupText, Text: " b"}}},
		{"key with format", "$VALUE|+=0$", []Segment{{Markup: MarkupKey, Text: "VALUE"}}},
		{"key that is empty", "$|x$", []Segment{{Markup: MarkupText, Text: "$|x$"}}},
		{"call", "[GetTitle(Scope.Foo).GetName]", []Segment{{Markup: MarkupCall, Text: "GetTitle(Scope.Foo).GetName"}}},
		{"nested call", "[A('[B]')]x", []Segment{{Markup: MarkupCall, Text: "A('[B]')"}, {Markup: MarkupText, Text: "x"}}},
		{"call that is blank", "a[ ]b", []Segment{{Markup: MarkupText, Text: "a[ ]b"}}},
		{"format", "#bold Words#! after", []Segment{
			{Markup: MarkupFormat, Text: "bold"}, {Markup: MarkupText, Text: "Words"}, {Markup: MarkupFormat},
			{Markup: MarkupText, Text: " after"},
		}},
		{"nested format", "#P a #N b#! c#!", []Segment{
			{Markup: MarkupFormat, Text: "P"}, {Markup: MarkupText, Text: "a "}, {Markup: MarkupFormat, Text: "N"},
			{Markup: MarkupText, Text: "b"}, {Markup: MarkupFormat}, {Markup: MarkupText, Text: " c"}, {Markup: MarkupFormat},
		}},
		{"format holding a call", "#tooltippable;tooltip:[GetPlayer.GetTooltipTag],liberal_tt Liberal#!", []Segment{
			{Markup: MarkupFormat, Text: "tooltippable;tooltip:[GetPlayer.GetTooltipTag],liberal_tt"},
			{Markup: MarkupCall, Text: "GetPlayer.GetTooltipTag"}, {Markup: MarkupText, Text: "Liberal"}, {Markup: MarkupFormat},
		}},
		{"format holding keys", "#TOOLTIP:$BREAKDOWN_TAG$ $VAL|-=2$#!", []Segment{
			{Markup: MarkupFormat, Text: "TOOLTIP:$BREAKDOWN_TAG$"}, {Markup: MarkupKey, Text: "BREAKDOWN_TAG"},
			{Markup: MarkupKey, Text: "VAL"}, {Markup: MarkupFormat},
		}},
		{"icon", "gold @gold_icon! more", []Segment{
			{Markup: MarkupText, Text: "gold "}, {Markup: MarkupIcon, Text: "gold_icon"}, {Markup: MarkupText, Text: " more"},
		}},
		{"break", `one\ntwo`, []Segment{{Markup: MarkupText, Text: "one"}, {Markup: MarkupBreak}, {Markup: MarkupText, Text: "two"}}},
		{"markers that open nothing", "cost $5 and [open # and @ end", []Segment{{Markup: MarkupText, Text: "cost $5 and [open # and @ end"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Segments(test.text, 100)
			same := func(segment, want Segment) bool { return segment.Markup == want.Markup && segment.Text == want.Text }
			if !slices.EqualFunc(got, test.want, same) {
				t.Errorf("segments = %v, want %v", got, test.want)
			}
			last := 100
			for _, segment := range got {
				if segment.Span.Start < last {
					t.Errorf("segment %+v starts before %d", segment, last)
				}
				last = segment.Span.Start
				if segment.Markup == MarkupCall && !strings.HasPrefix(test.text[segment.Span.Start-100:], "["+segment.Text+"]") {
					t.Errorf("call %+v is not at its span", segment)
				}
			}
			if end := got[len(got)-1].Span.End; end != 100+len(test.text) {
				t.Errorf("segments end at %d, want %d", end, 100+len(test.text))
			}
		})
	}
}

// TestToPlainDropsMarkup checks that the plain text keeps the text and the breaks and drops every other markup.
func TestToPlainDropsMarkup(t *testing.T) {
	got := ToPlain(Segments(`#bold Gain $AMOUNT$[Scope.GetName]@gold!#!\nnext`, 0))
	if want := "Gain \nnext"; got != want {
		t.Errorf("plain = %q, want %q", got, want)
	}
	tooltip := "#tooltippable;tooltip:[GetPlayer.GetTooltipTag],liberal_tt Liberal#!"
	if got := ToPlain(Segments(tooltip, 0)); got != "Liberal" {
		t.Errorf("plain of a tooltip format = %q, want Liberal", got)
	}
}

// TestEncodeMessages checks the text written for two messages, and that it parses back to them.
func TestEncodeMessages(t *testing.T) {
	got := Encode(english, []Message{{Key: "a", Text: "One", Version: 1}, {Key: "b", Text: `Two "2"`}})
	want := "l_english:\n a:1 \"One\"\n b:0 \"Two \"2\"\"\n"
	if got != want {
		t.Errorf("encoded = %q, want %q", got, want)
	}
	if file := Parse(got, ""); len(file.Messages) != 2 || len(file.Errors) != 0 || file.Language != english {
		t.Errorf("encoded text parses to %+v", file)
	}
}

// TestParseInstalls parses every localization file of each install and logs the counts and every malformed line. It
// skips a game whose install root is not set in its environment variable.
func TestParseInstalls(t *testing.T) {
	for game, variable := range map[string]string{"ck3": "PMT_TEST_INSTALL_CK3", "vic3": "PMT_TEST_INSTALL_VIC3", "eu5": "PMT_TEST_INSTALL_EU5"} {
		t.Run(game, func(t *testing.T) {
			root := os.Getenv(variable)
			if root == "" {
				t.Skipf("%s is not set", variable)
			}
			list, err := files.Walk(files.Root{Dir: root}, nil)
			if err != nil {
				t.Fatal(err)
			}
			count, messages, segments, undecided := 0, 0, 0, 0
			languages := map[string]int{}
			var malformed []string
			for _, listed := range list {
				if listed.Format != files.FormatLocalization {
					continue
				}
				text, err := files.ReadText(listed.Path)
				if err != nil {
					t.Fatal(err)
				}
				file := Parse(text.Text, listed.Path)
				count++
				messages += len(file.Messages)
				languages[file.Language]++
				if file.Language == "" {
					undecided++
				}
				for _, message := range file.Messages {
					segments += len(Segments(message.Text, message.TextSpan.Start))
				}
				for _, parseError := range file.Errors {
					malformed = append(malformed, fmt.Sprintf("%s:%d", listed.Path, file.Lines.ToPosition(parseError.Span.Start).Line+1))
				}
				if count%2000 == 0 {
					t.Logf("%s: %d files read", game, count)
				}
			}
			t.Logf("%s: %d files, %d messages, %d segments, %d without a language, %d malformed lines, languages %v",
				game, count, messages, segments, undecided, len(malformed), languages)
			for _, line := range malformed {
				t.Logf("%s: malformed line at %s", game, line)
			}
		})
	}
}
