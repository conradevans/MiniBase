package explorer

type ObjectType string

const (
	ObjectTypeTable            ObjectType = "table"
	ObjectTypeView             ObjectType = "view"
	ObjectTypeMaterializedView ObjectType = "materialized_view"
)

type Object struct {
	Name string     `json:"name"`
	Type ObjectType `json:"type"`
}

type Schema struct {
	Name    string   `json:"name"`
	Objects []Object `json:"objects"`
}

type Catalog struct {
	Schemas []Schema `json:"schemas"`
}

type Column struct {
	Name            string `json:"name"`
	DataType        string `json:"dataType"`
	Nullable        bool   `json:"nullable"`
	PrimaryKey      bool   `json:"primaryKey"`
	OrdinalPosition int    `json:"ordinalPosition"`
}

type Description struct {
	Schema  string   `json:"schema"`
	Object  Object   `json:"object"`
	Columns []Column `json:"columns"`
}

type CellKind string

const (
	CellKindValue  CellKind = "value"
	CellKindJSON   CellKind = "json"
	CellKindNull   CellKind = "null"
	CellKindBinary CellKind = "binary"
)

type Cell struct {
	Kind      CellKind `json:"kind"`
	Value     string   `json:"value,omitempty"`
	Truncated bool     `json:"truncated"`
}

type RowsPage struct {
	Schema  string   `json:"schema"`
	Object  Object   `json:"object"`
	Columns []string `json:"columns"`
	Rows    [][]Cell `json:"rows"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
	HasMore bool     `json:"hasMore"`
}
