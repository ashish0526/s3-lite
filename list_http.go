package s3lite

import (
	"encoding/xml"
	"net/http"
	"strconv"
)

type listBucketContent struct {
	Key  string `xml:"Key"`
	Size int64  `xml:"Size"`
	ETag string `xml:"ETag"`
}

type listBucketCommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

type listBucketResult struct {
	XMLName               xml.Name                 `xml:"ListBucketResult"`
	Name                  string                   `xml:"Name"`
	Prefix                string                   `xml:"Prefix"`
	Delimiter             string                   `xml:"Delimiter,omitempty"`
	MaxKeys               int                      `xml:"MaxKeys"`
	IsTruncated           bool                     `xml:"IsTruncated"`
	NextContinuationToken string                   `xml:"NextContinuationToken,omitempty"`
	Contents              []listBucketContent      `xml:"Contents"`
	CommonPrefixes        []listBucketCommonPrefix `xml:"CommonPrefixes"`
}

func (s *Server) listObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	q := r.URL.Query()
	maxKeys, _ := strconv.Atoi(q.Get("max-keys"))
	res, err := s.store.List(bucket, q.Get("prefix"), q.Get("delimiter"), q.Get("continuation-token"), maxKeys)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	out := listBucketResult{
		Name:                  bucket,
		Prefix:                q.Get("prefix"),
		Delimiter:             q.Get("delimiter"),
		MaxKeys:               maxKeys,
		IsTruncated:           res.IsTruncated,
		NextContinuationToken: res.NextContinuationToken,
	}
	for _, o := range res.Objects {
		out.Contents = append(out.Contents, listBucketContent{Key: o.Key, Size: o.Size, ETag: quoteETag(o.ETag)})
	}
	for _, p := range res.CommonPrefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, listBucketCommonPrefix{Prefix: p})
	}

	w.Header().Set("Content-Type", "application/xml")
	xml.NewEncoder(w).Encode(out)
}
