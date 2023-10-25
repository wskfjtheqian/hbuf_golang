package hbuf

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"testing"
	"time"
)
import _ "net/http/pprof"

func Test_Tag(t *testing.T) {
	for i := 1; i <= 4; i++ {
		for t := 0; t <= 7; t++ {
			for l := 1; l <= 4; l++ {
				val := FormatTag(uint8(i), HbufType(t), uint8(l))
				println(strconv.FormatInt(int64(val), 2))
				idLen, typ, typLen := ParseTag(val)
				r := "false"
				if uint8(i) == idLen && HbufType(t) == typ && uint8(l) == typLen {
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
	FormatIntPrt(&buf, 12, &val)

	println(buf.Len())
}

type AS struct {
	Age  int64  `json:"age,omitempty"`
	Name string `json:"name"`
}

func (a *AS) ToData() ([]byte, error) {
	buffer := bytes.Buffer{}
	FormatIntPrt(&buffer, 4, &a.Age)
	FormatBytes(&buffer, 5, []byte(a.Name))
	return buffer.Bytes(), nil
}
func (a *AS) FormData(buf []byte, start uint32, len uint32) error {
	return Parse(buf, &start, len, func(id uint32) ParseHandle {
		switch id {
		case 4:
			return ParseInt64(&a.Age)
		case 5:
			return ParseString(&a.Name)
		}
		return nil
	})
}

type BS struct {
	Id int64 `json:"id,omitempty"`

	A AS `json:"a"`

	Courses []string `json:"courses"`
}

func (b *BS) ToData() ([]byte, error) {
	buffer := bytes.Buffer{}
	FormatIntPrt(&buffer, 1, &b.Id)
	FormatData(&buffer, 2, &b.A)
	FormatList(&buffer, 3, b.Courses, func(buf *bytes.Buffer, id uint32, val string) {
		FormatBytes(&buffer, id, []byte(val))
	})
	return buffer.Bytes(), nil
}

func (b *BS) FormData(buf []byte, start uint32, len uint32) error {
	return Parse(buf, &start, len, func(id uint32) ParseHandle {
		switch id {
		case 1:
			return ParseInt64(&b.Id)
		case 2:
			return ParseData(&b.A)
		case 3:
			return ParseList(b.Courses)
		}
		return nil
	})
}

func Test_Data(t *testing.T) {
	go func() {
		log.Println(http.ListenAndServe("0.0.0.0:9090", nil))
	}()

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
		return
	}
	println(hex.EncodeToString(data))
	println(len(data))

	bb := BS{}
	err = bb.FormData(data, 0, uint32(len(data)))
	if err != nil {
		log.Println(err.Error())
		return
	}

	marshal, err := json.Marshal(&b)
	if err != nil {
		return
	}
	println(string(marshal))
	println(len(marshal))

	ti := time.Now().UnixMilli()
	for i := 0; i < 1000000; i++ {
		b.ToData()
	}
	println(time.Now().UnixMilli() - ti)

	ti = time.Now().UnixMilli()
	for i := 0; i < 1000000; i++ {
		json.Marshal(&b)
	}
	println(time.Now().UnixMilli() - ti)

	ti = time.Now().UnixMilli()
	for i := 0; i < 1000; i++ {
		bb.FormData(data, 0, uint32(len(data)))
	}
	println(time.Now().UnixMilli() - ti)

	ti = time.Now().UnixMilli()
	for i := 0; i < 1000000; i++ {
		json.Unmarshal(marshal, &bb)
	}
	println(time.Now().UnixMilli() - ti)
	select {}
}
