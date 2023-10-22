package hbuf

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"log"
	"strconv"
	"testing"
	"time"
)

func Test_Tag(t *testing.T) {
	for i := 1; i <= 4; i++ {
		for t := 0; t <= 15; t++ {
			for l := 1; l <= 4; l++ {
				val := FormatTag(uint8(i), HbufType(t), uint8(l))
				println(strconv.FormatInt(int64(val), 2))
				idLen, typ, typLen := ParseTag(val)
				r := "false"
				if uint8(i) == idLen && uint8(t) == typ && uint8(l) == typLen {
					r = "true"
				}
				println(strconv.Itoa(int(i)) + "," + strconv.Itoa(int(t)) + "," + strconv.Itoa(int(l)) + "=>" + r)
				println(strconv.Itoa(int(idLen)) + "," + strconv.Itoa(int(typ)) + "," + strconv.Itoa(int(typLen)) + "=>" + r)
			}
		}
	}
}

func Test_Int(t *testing.T) {
	val := int64(0xFF00FF8812880)
	buf := bytes.Buffer{}
	FormatInt(&buf, 12, &val)

	println(buf.Len())
}

type AS struct {
	Age  int64  `json:"age,omitempty"`
	Name string `json:"name"`
}

func (a *AS) ToData() ([]byte, error) {
	buffer := bytes.Buffer{}
	FormatInt(&buffer, 4, &a.Age)
	FormatBytes(&buffer, 5, []byte(a.Name))
	return buffer.Bytes(), nil
}
func (a *AS) FormData([]byte) error {
	return nil
}

type BS struct {
	Id int64 `json:"id,omitempty"`

	A AS `json:"a"`

	Courses []string `json:"courses"`
}

func (b *BS) ToData() ([]byte, error) {
	buffer := bytes.Buffer{}
	FormatInt(&buffer, 1, &b.Id)
	FormatData(&buffer, 2, &b.A)
	FormatList(&buffer, 3, b.Courses, func(buf *bytes.Buffer, id int32, val string) {
		FormatBytes(&buffer, id, []byte(val))
	})
	return buffer.Bytes(), nil
}

func Test_Data(t *testing.T) {
	b := BS{
		Id: 12,
		A: AS{
			Age:  128,
			Name: "开开向上",
		},
		Courses: []string{
			"aaa",
			"bbb",
		},
	}
	data, err := b.ToData()
	if err != nil {
		log.Println(err.Error())
	}
	println(hex.EncodeToString(data))
	println(len(data))

	marshal, err := json.Marshal(&b)
	if err != nil {
		return
	}
	println(string(marshal))
	println(len(marshal))

	ti := time.Now().UnixMilli()
	for i := 0; i < 10000000; i++ {
		b.ToData()
	}
	println(time.Now().UnixMilli() - ti)

	ti = time.Now().UnixMilli()
	for i := 0; i < 10000000; i++ {
		json.Marshal(&b)
	}
	println(time.Now().UnixMilli() - ti)
}
