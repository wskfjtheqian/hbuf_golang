package data_hbuf_test

// Status 12
type Status int

// StatusEnable 启用
const StatusEnable Status = 0

// StatusDisabled 禁用
const StatusDisabled Status = 1

func (e Status) Pointer() *Status {
	pointer := e
	return &pointer
}

var statusMap = map[Status]string{
	StatusEnable:   "Enable",
	StatusDisabled: "Disabled",
}

func (e Status) ToName() string {
	return statusMap[e]
}

var statusValues = map[string]Status{
	"Enable":   StatusEnable,
	"Disabled": StatusDisabled,
}

func StatusValues() map[string]Status {
	return statusValues
}
