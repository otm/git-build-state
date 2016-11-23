package main

import (
	"bytes"
	"encoding/base64"
	"go/format"
	"io"
	"io/ioutil"
	"log"
	"os"
)

// Reads all .txt files in the current folder
// and encodes them as strings literals in textfiles.go
func main() {
	fs, _ := ioutil.ReadDir("assets")
	outFile, _ := os.Create("assets-generated.go")
	var out bytes.Buffer
	out.Write([]byte("package main\n"))
	out.Write([]byte("import \"encoding/base64\"\n"))
	out.Write([]byte("func asset(key string) (value []byte) {\n"))
	out.Write([]byte("a := map[string]string{\n"))
	for _, f := range fs {
		out.Write([]byte("\"" + f.Name() + "\": \""))
		f, _ := os.Open("assets/" + f.Name())
		encoder := base64.NewEncoder(base64.StdEncoding, &out)
		io.Copy(encoder, f)
		encoder.Close()
		out.Write([]byte("\",\n"))
	}
	out.Write([]byte("}\n"))
	out.Write([]byte("res, _ := base64.StdEncoding.DecodeString(a[key])\n"))
	out.Write([]byte("return res"))
	out.Write([]byte("}\n"))

	out2, err := format.Source(out.Bytes())
	if err != nil {
		log.Fatal(err)
	}

	out.Reset()
	out.Write(out2)
	io.Copy(outFile, &out)
	outFile.Close()

}
