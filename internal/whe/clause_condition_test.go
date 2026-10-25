package whe

import (
	"bytes"
	"encoding/binary"
	"testing"

	"dow-mod-studio/internal/chunky"
)

func clauseConditionTestWhe(children ...*chunky.Chunk) []byte {
	var out bytes.Buffer
	out.Write(chunky.Preamble())
	out.Write(chunky.Serialize(&chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: children}))
	return out.Bytes()
}

func clauseTestPayload(t *testing.T, kind, comparison int32, body []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, kind)
	writeTestString(&b, "Health")
	_ = binary.Write(&b, binary.LittleEndian, comparison)
	b.Write(body)
	return b.Bytes()
}

func writeTestString(b *bytes.Buffer, value string) {
	_ = binary.Write(b, binary.LittleEndian, int32(len(value)))
	b.WriteString(value)
}

func TestClauseFloatParseEditRoundTrip(t *testing.T) {
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, float32(1.25))
	_ = binary.Write(&body, binary.LittleEndian, float32(2.5))
	body.WriteByte(0xA5)
	clauseFolder := &chunky.Chunk{TypeID: "FOLDCLST", Version: 1, Children: []*chunky.Chunk{{TypeID: "DATACLAS", Version: 2, Name: "health", Payload: clauseTestPayload(t, ClauseFloat, ClauseGreaterOrEqual, body.Bytes())}}}
	// Same-named definitions outside the expected folder are not loaded or edited.
	ignored := &chunky.Chunk{TypeID: "DATACLAS", Version: 2, Name: "unrelated", Payload: clauseTestPayload(t, ClauseString, ClauseEqual, nil)}
	doc, err := ParseDocument(clauseConditionTestWhe(clauseFolder, ignored))
	if err != nil {
		t.Fatal(err)
	}
	clauses := doc.Clauses()
	if len(clauses) != 1 {
		t.Fatalf("Clauses() = %d, want 1", len(clauses))
	}
	clause := clauses[0]
	if clause.Name != "health" || clause.Type != ClauseFloat || clause.Variable != "Health" || clause.Comparison != ClauseGreaterOrEqual || clause.FloatValue != 125 || clause.FloatDefault != 250 || !bytes.Equal(clause.trailing, []byte{0xA5}) {
		t.Fatalf("parsed clause = %+v", clause)
	}
	clause.Variable, clause.FloatValue, clause.FloatDefault = "CurrentHealth", 375, 450
	if err := doc.SetClause("health", clause); err != nil {
		t.Fatal(err)
	}
	data, err := doc.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	gotDoc, err := ParseDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	got := gotDoc.Clauses()[0]
	if got.Variable != "CurrentHealth" || got.FloatValue != 375 || got.FloatDefault != 450 || !bytes.Equal(got.trailing, []byte{0xA5}) {
		t.Fatalf("round-trip clause = %+v", got)
	}
}

func TestClauseBooleanAndStringPayloads(t *testing.T) {
	var boolBody bytes.Buffer
	boolBody.Write([]byte{1, 0})
	boolClause, err := parseClause("enabled", clauseTestPayload(t, ClauseBoolean, ClauseNotEqual, boolBody.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !boolClause.BoolValue || boolClause.BoolDefault || boolClause.Comparison != ClauseNotEqual {
		t.Fatalf("boolean clause = %+v", boolClause)
	}
	boolClause.BoolDefault = true
	encodedBool, err := marshalClause(boolClause)
	if err != nil {
		t.Fatal(err)
	}
	decodedBool, err := parseClause("enabled", encodedBool)
	if err != nil || !decodedBool.BoolValue || !decodedBool.BoolDefault {
		t.Fatalf("boolean round-trip = %+v, %v", decodedBool, err)
	}

	var stringBody bytes.Buffer
	writeTestString(&stringBody, "ready")
	writeTestString(&stringBody, "waiting")
	stringClause, err := parseClause("state", clauseTestPayload(t, ClauseString, ClauseEqual, stringBody.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if stringClause.StringValue != "ready" || stringClause.StringDefault != "waiting" {
		t.Fatalf("string clause = %+v", stringClause)
	}
	stringClause.StringValue = "active"
	encoded, err := marshalClause(stringClause)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := parseClause("state", encoded)
	if err != nil || decoded.StringValue != "active" || decoded.StringDefault != "waiting" {
		t.Fatalf("string round-trip = %+v, %v", decoded, err)
	}
}

func TestConditionsReferenceClausesRoundTrip(t *testing.T) {
	var conditionPayload bytes.Buffer
	_ = binary.Write(&conditionPayload, binary.LittleEndian, int32(2))
	writeTestString(&conditionPayload, "health")
	writeTestString(&conditionPayload, "is_alive")
	conditionPayload.WriteByte(0xCC)
	conditionFolder := &chunky.Chunk{TypeID: "FOLDCONL", Version: 1, Children: []*chunky.Chunk{{TypeID: "DATACOND", Version: 1, Name: "can_fight", Payload: conditionPayload.Bytes()}}}
	wrongPlace := &chunky.Chunk{TypeID: "DATACOND", Version: 1, Name: "ignored", Payload: conditionPayload.Bytes()}
	doc, err := ParseDocument(clauseConditionTestWhe(conditionFolder, wrongPlace))
	if err != nil {
		t.Fatal(err)
	}
	conditions := doc.Conditions()
	if len(conditions) != 1 || conditions[0].Name != "can_fight" || len(conditions[0].ClauseNames) != 2 || conditions[0].ClauseNames[0] != "health" || conditions[0].ClauseNames[1] != "is_alive" || !bytes.Equal(conditions[0].trailing, []byte{0xCC}) {
		t.Fatalf("conditions = %+v", conditions)
	}
	condition := conditions[0]
	condition.ClauseNames = []string{"health", "is_alive", "can_move"}
	if err := doc.SetCondition("can_fight", condition); err != nil {
		t.Fatal(err)
	}
	data, err := doc.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := ParseDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	got := reparsed.Conditions()[0]
	if len(got.ClauseNames) != 3 || got.ClauseNames[2] != "can_move" || !bytes.Equal(got.trailing, []byte{0xCC}) {
		t.Fatalf("edited condition = %+v", got)
	}
}
