// SPDX-License-Identifier: MIT

package runner

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/otfabric/profinetctl/internal/errs"
	"github.com/otfabric/profinetctl/internal/format"
)

type text string

func (t text) MarshalTextOutput() (string, error) { return string(t), nil }

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

func TestOutputAndWrite(t *testing.T) {
	inv := &Invocation{}
	ctx := WithInvocation(context.Background(), inv)
	if InvocationFrom(context.Background()) != nil || InvocationFrom(ctx) != inv {
		t.Fatal("invocation")
	}
	f, err := Output(ctx, "json")
	if err != nil || f != format.FormatJSON || !inv.HasOutputFormat || inv.OutputFormat != format.FormatJSON {
		t.Fatalf("output %v %v %+v", f, err, inv)
	}
	if _, err := Output(ctx, "xml"); err == nil {
		t.Fatal("xml accepted")
	}
	if _, err := Output(context.Background(), "text"); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := Write(ctx, &b, format.FormatText, text("done"), func(any) int { return errs.ExitPartial }); err != nil || b.String() != "done\n" || inv.SuccessExitCode != errs.ExitPartial {
		t.Fatalf("write %v %q %+v", err, b.String(), inv)
	}
	if err := Write(context.Background(), &b, format.FormatText, text("x"), nil); err != nil {
		t.Fatal(err)
	}
	for _, f := range []format.OutputFormat{format.FormatTable, format.FormatText} {
		err := Write(ctx, failing{}, f, text("x"), nil)
		if e, ok := errs.As(err); !ok || e.Kind != errs.KindOutput {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestRenderFatal(t *testing.T) {
	var out, errb bytes.Buffer
	err := errs.New(errs.KindTimeout, errs.CodeRequestTimeout, "no answer", errors.New("deadline"))
	if code := RenderFatal(nil, err, &out, &errb, true); code != errs.ExitTimeout || errb.String() != "error: no answer\ncause: deadline\n" || out.Len() != 0 {
		t.Fatalf("text: %d %q %q", code, out.String(), errb.String())
	}
	out.Reset()
	errb.Reset()
	inv := &Invocation{}
	inv.SetOutputFormat(format.FormatJSON)
	if code := RenderFatal(inv, errors.New("plain"), &out, &errb, false); code != errs.ExitInternal || !strings.Contains(out.String(), `"code": "internal_unexpected"`) {
		t.Fatalf("json: %d %q", code, out.String())
	}
	if code := RenderFatal(inv, err, failing{}, &errb, false); code != errs.ExitInternal || !strings.Contains(errb.String(), "write stdout") {
		t.Fatalf("failing stdout: %d %q", code, errb.String())
	}
	if i := Info(errs.New(errs.KindUsage, "c", "", errors.New("from cause"))); i.Message != "from cause" {
		t.Fatalf("info %+v", i)
	}
}

func TestFormatArg(t *testing.T) {
	for _, c := range []struct {
		args  []string
		value string
		found bool
	}{
		{[]string{"x", "--format", "json"}, "json", true},
		{[]string{"-o", "table", "--format=text"}, "text", true},
		{[]string{"-o=json"}, "json", true},
		{[]string{"--", "--format", "json"}, "", false},
		{[]string{"--format"}, "", false},
		{nil, "", false},
	} {
		if v, ok := FormatArg(c.args); v != c.value || ok != c.found {
			t.Errorf("%v: %q %v", c.args, v, ok)
		}
	}
}
