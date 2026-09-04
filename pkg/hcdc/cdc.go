package hcdc

type Schema string
type Table string
type Column string
type SchemaTable string
type RawBytes []byte

type ColumnInfo struct {
	Name    string
	Type    string
	Comment string
	IsKey   bool
}

func (i ColumnInfo) String() string {
	return i.Name + ":" + i.Type
}
