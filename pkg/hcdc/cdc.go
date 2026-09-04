package hcdc

type Schema string
type Table string
type Column string
type SchemaTable string
type RawBytes []byte

type ColumnInfo struct {
	Name string
	Type string
}

func (i ColumnInfo) String() string {
	return i.Name + ":" + i.Type
}
