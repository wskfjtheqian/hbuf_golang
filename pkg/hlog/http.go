package hlog

type httpWriter struct {
}

func (h httpWriter) Compare(call func(v1 Level, v2 Level) bool) {

}

func newHttpWriter(string, Level) *httpWriter {
	return &httpWriter{}
}

func (h httpWriter) Flush() error {
	return nil
}

func (h httpWriter) Sync() error {
	return nil
}

func (h httpWriter) Write(level Level, p []byte) (n int, err error) {
	return 0, nil
}
