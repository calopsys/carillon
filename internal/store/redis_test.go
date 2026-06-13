package store

import "testing"

func TestMarshalMarkRoundTrip(t *testing.T) {
	in := Mark{
		Name:       "helm",
		Source:     "github",
		Identifier: "helm/helm",
		Tag:        "v4.2.1",
		Key:        []int64{4, 2, 1},
		Spec:       "deadbeefdeadbeef",
	}
	raw, err := marshalMark(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := unmarshalMark(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Source != in.Source || out.Identifier != in.Identifier ||
		out.Tag != in.Tag || out.Spec != in.Spec || len(out.Key) != len(in.Key) {
		t.Fatalf("round-trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
	for i := range in.Key {
		if out.Key[i] != in.Key[i] {
			t.Fatalf("key[%d] = %d, want %d", i, out.Key[i], in.Key[i])
		}
	}
}

func TestNoOpIsStateless(t *testing.T) {
	var s Store = NoOp{}
	if _, found, err := s.Get(t.Context(), "x"); found || err != nil {
		t.Fatalf("NoOp.Get should never find: found=%v err=%v", found, err)
	}
	if err := s.Upsert(t.Context(), Mark{Name: "x"}); err != nil {
		t.Fatalf("NoOp.Upsert should discard: %v", err)
	}
}
