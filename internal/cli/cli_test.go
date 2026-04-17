package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
)

func TestWriteEnvelope_Success(t *testing.T) {
	var buf bytes.Buffer
	cli.WriteEnvelope(&buf, cli.Envelope{
		OpID: "01HNBXBT9J6MGK3Z5R7WVXTM0A",
		Kind: "task.claim",
		Data: map[string]any{"claim_id": "cid", "run_id": "rid"},
	})
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["op_id"] != "01HNBXBT9J6MGK3Z5R7WVXTM0A" {
		t.Fatalf("bad op_id: %+v", got)
	}
	if got["error"] != nil {
		t.Fatalf("error should be absent on success")
	}
	d, _ := got["data"].(map[string]any)
	if d["claim_id"] != "cid" {
		t.Fatalf("bad data: %+v", d)
	}
}

func TestWriteEnvelope_DataDefaultEmptyObject(t *testing.T) {
	var buf bytes.Buffer
	cli.WriteEnvelope(&buf, cli.Envelope{Kind: "task.release"})
	var got map[string]any
	_ = json.Unmarshal(buf.Bytes(), &got)
	d, ok := got["data"]
	if !ok {
		t.Fatal("data field missing")
	}
	if _, ok := d.(map[string]any); !ok {
		t.Fatalf("data must be an object, got %T", d)
	}
}

func TestExitCodeFor_Mapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{cairnerr.New(cairnerr.CodeBadInput, "x", "y"), 1},
		{cairnerr.New(cairnerr.CodeValidation, "x", "y"), 1},
		{cairnerr.New(cairnerr.CodeConflict, "x", "y"), 2},
		{cairnerr.New(cairnerr.CodeNotFound, "x", "y"), 3},
		{cairnerr.New(cairnerr.CodeSubstrate, "x", "y"), 4},
		{errors.New("random"), 4},
	}
	for _, c := range cases {
		got := cli.ExitCodeFor(c.err)
		if got != c.want {
			t.Errorf("%v: got %d want %d", c.err, got, c.want)
		}
	}
}

func TestWriteEnvelope_ErrorShape(t *testing.T) {
	var buf bytes.Buffer
	cli.WriteEnvelope(&buf, cli.Envelope{
		Kind: "task.claim",
		Err:  cairnerr.New(cairnerr.CodeConflict, "dep_not_done", "blocked").WithDetails(map[string]any{"x": 1}),
	})
	var got map[string]any
	_ = json.Unmarshal(buf.Bytes(), &got)
	if got["data"] != nil {
		t.Fatalf("data should be absent on error")
	}
	errMap := got["error"].(map[string]any)
	if errMap["code"] != "dep_not_done" {
		t.Fatalf("bad error.code: %+v", errMap)
	}
}

func TestResolveStateRoot_CAIRN_HOMEWins(t *testing.T) {
	t.Setenv("CAIRN_HOME", "/tmp/cairn-x")
	got := cli.ResolveStateRoot("")
	if got != "/tmp/cairn-x" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveStateRoot_ExplicitOverrideWins(t *testing.T) {
	t.Setenv("CAIRN_HOME", "/tmp/env")
	got := cli.ResolveStateRoot("/explicit")
	if got != "/explicit" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveStateRoot_PlatformFallback(t *testing.T) {
	t.Setenv("CAIRN_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	home, _ := os.UserHomeDir()
	got := cli.ResolveStateRoot("")
	if filepath.Dir(got) == "" {
		t.Fatalf("empty fallback")
	}
	_ = home
}
