package utl

import (
	"strings"
)

func Black(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;30m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

func Red(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;31m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

func Green(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;32m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

func Yellow(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;33m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

func Blue(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;34m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

func Purple(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;35m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

func Cyan(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;36m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}

func White(str string) string {
	ret := strings.Builder{}
	ret.WriteString("\u001B[0;38m")
	ret.WriteString(str)
	ret.WriteString("\u001B[0m")
	return ret.String()
}
