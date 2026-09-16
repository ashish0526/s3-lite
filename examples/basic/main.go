// Command basic is a tiny end-to-end demo of s3-lite's HTTP API: it starts
// a real server on a loopback port and drives it with plain net/http
// requests, the same way any S3 SDK would.
package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	s3lite "github.com/ashish0526/s3-lite"
)

func main() {
	dir, err := os.MkdirTemp("", "s3lite-demo-")
	must(err)
	defer os.RemoveAll(dir)

	store, err := s3lite.NewStore(dir)
	must(err)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	defer ln.Close()
	go http.Serve(ln, s3lite.NewServer(store))
	base := "http://" + ln.Addr().String()

	req(http.MethodPut, base+"/demo", "", nil)
	fmt.Println("created bucket \"demo\"")

	put := req(http.MethodPut, base+"/demo/hello.txt", "hello, s3-lite", nil)
	fmt.Printf("PUT hello.txt -> ETag %s, version %s\n", put.Header.Get("ETag"), put.Header.Get("x-amz-version-id"))

	// Multipart upload: two parts, uploaded out of order, assembled by
	// CompleteMultipartUpload into one object.
	var initiated struct {
		UploadID string `xml:"UploadId"`
	}
	initResp := req(http.MethodPost, base+"/demo/big.bin?uploads", "", nil)
	must(xml.NewDecoder(initResp.Body).Decode(&initiated))

	part2 := req(http.MethodPut, fmt.Sprintf("%s/demo/big.bin?partNumber=2&uploadId=%s", base, initiated.UploadID), "world", nil)
	part1 := req(http.MethodPut, fmt.Sprintf("%s/demo/big.bin?partNumber=1&uploadId=%s", base, initiated.UploadID), "hello ", nil)
	completeBody := fmt.Sprintf(
		`<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part><Part><PartNumber>2</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>`,
		part1.Header.Get("ETag"), part2.Header.Get("ETag"),
	)
	req(http.MethodPost, fmt.Sprintf("%s/demo/big.bin?uploadId=%s", base, initiated.UploadID), completeBody, nil)
	get := req(http.MethodGet, base+"/demo/big.bin", "", nil)
	body, _ := io.ReadAll(get.Body)
	fmt.Printf("multipart big.bin -> %q (ETag %s, note the \"-2\" suffix)\n", body, get.Header.Get("ETag"))

	// Listing with a prefix.
	list := req(http.MethodGet, base+"/demo?list-type=2&prefix=hello", "", nil)
	listBody, _ := io.ReadAll(list.Body)
	fmt.Printf("ListObjectsV2(prefix=hello) -> %s\n", strings.TrimSpace(string(listBody)))

	// Versioning: overwrite, then read both the old and new version.
	v1ETag := put.Header.Get("ETag")
	v1ID := put.Header.Get("x-amz-version-id")
	req(http.MethodPut, base+"/demo/hello.txt", "hello, v2", nil)
	old := req(http.MethodGet, fmt.Sprintf("%s/demo/hello.txt?versionId=%s", base, v1ID), "", nil)
	oldBody, _ := io.ReadAll(old.Body)
	fmt.Printf("old version %s still reads back as %q\n", v1ID, oldBody)

	// Conditional overwrite: this If-Match uses the *original* ETag, which
	// is now stale because the update above already moved the key to v2 —
	// exactly the lost-update race a conditional PUT exists to catch.
	stale := req(http.MethodPut, base+"/demo/hello.txt", "conflicting write", map[string]string{"If-Match": v1ETag})
	fmt.Printf("stale conditional PUT -> HTTP %d (rejected, as expected)\n", stale.StatusCode)
}

func req(method, url, body string, headers map[string]string) *http.Response {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	httpReq, err := http.NewRequest(method, url, r)
	must(err)
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	must(err)
	return resp
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
