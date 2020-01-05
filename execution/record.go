package execution

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"

	"github.com/cube2222/octosql"
)

type Field struct {
	Name octosql.VariableName
}

type metadata struct {
	id             ID
	undo           bool
	eventTimeField octosql.VariableName
}

type ID struct {
	id string
}

func NewID(id string) ID {
	return ID{
		id: id,
	}
}

func (id ID) String() string {
	return id.id
}

type Record struct {
	metadata   metadata
	fieldNames []octosql.VariableName
	data       []octosql.Value
}

type RecordOption func(stream *Record)

func WithUndo() RecordOption {
	return func(r *Record) {
		r.metadata.undo = true
	}
}

func WithEventTimeField(field octosql.VariableName) RecordOption {
	return func(r *Record) {
		r.metadata.eventTimeField = field
	}
}

func WithMetadataFrom(base *Record) RecordOption {
	return func(r *Record) {
		r.metadata = base.metadata
	}
}

func WithID(id ID) RecordOption {
	return func(rec *Record) {
		rec.metadata.id = id
	}
}

func NewRecord(fields []octosql.VariableName, data map[octosql.VariableName]octosql.Value, opts ...RecordOption) *Record {
	dataInner := make([]octosql.Value, len(fields))
	for i := range fields {
		dataInner[i] = data[fields[i]]
	}
	return NewRecordFromSlice(fields, dataInner, opts...)
}

func NewRecordFromSlice(fields []octosql.VariableName, data []octosql.Value, opts ...RecordOption) *Record {
	r := &Record{
		fieldNames: fields,
		data:       data,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

func (r *Record) Value(field octosql.VariableName) octosql.Value {
	if field.Source() == "sys" {
		switch field.Name() {
		case "undo":
			return octosql.MakeBool(r.IsUndo())
		case "event_time":
			return r.EventTime()
		default:
			return octosql.MakeNull()
		}
	}
	for i := range r.fieldNames {
		if r.fieldNames[i] == field {
			return r.data[i]
		}
	}
	return octosql.MakeNull()
}

func (r *Record) Fields() []Field {
	fields := make([]Field, 0)
	for _, fieldName := range r.fieldNames {
		fields = append(fields, Field{
			Name: fieldName,
		})
	}
	if len(r.metadata.eventTimeField.String()) != 0 {
		fields = append(fields, Field{
			Name: octosql.NewVariableName("sys.event_time"),
		})
	}
	if r.IsUndo() {
		fields = append(fields, Field{
			Name: octosql.NewVariableName("sys.undo"),
		})
	}

	return fields
}

func (r *Record) AsVariables() octosql.Variables {
	out := make(octosql.Variables)
	for i := range r.fieldNames {
		out[r.fieldNames[i]] = r.data[i]
	}

	return out
}

func (r *Record) AsTuple() octosql.Value {
	return octosql.MakeTuple(r.data)
}

func (r *Record) Equal(other *Record) bool {
	myFields := r.Fields()
	otherFields := other.Fields()
	if len(myFields) != len(otherFields) {
		return false
	}

	for i := range myFields {
		if myFields[i] != otherFields[i] {
			return false
		}
		if !octosql.AreEqual(r.Value(myFields[i].Name), other.Value(myFields[i].Name)) {
			return false
		}
	}

	if r.metadata.eventTimeField.String() != other.metadata.eventTimeField.String() {
		return false
	}

	if r.metadata.undo != other.metadata.undo {
		return false
	}

	return true
}

func (r *Record) String() string {
	parts := make([]string, len(r.fieldNames))
	for i := range r.fieldNames {
		parts[i] = fmt.Sprintf("%s: %s", r.fieldNames[i].String(), r.data[i].String())
	}

	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

func (r *Record) IsUndo() bool {
	return r.metadata.undo
}

func (r *Record) EventTime() octosql.Value {
	return r.Value(r.metadata.eventTimeField)
}

func (r *Record) ID() ID {
	return r.metadata.id
}

type RecordStream interface {
	Next(ctx context.Context) (*Record, error)
	io.Closer
}

var ErrEndOfStream = errors.New("end of stream")

var ErrNotFound = errors.New("not found")
