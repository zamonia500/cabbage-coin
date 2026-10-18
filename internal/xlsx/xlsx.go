// Package xlsx reads cell text from the first worksheet of an .xlsx file.
// It handles only what the government spreadsheets we consume need:
// shared strings, inline strings and plain values.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Row maps a column letter ("A", "AB") to the cell's text.
type Row map[string]string

// Rows returns every row of the first worksheet, in sheet order.
func Rows(data []byte) ([]Row, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	ss, err := sharedStrings(z)
	if err != nil {
		return nil, err
	}
	f, err := open(z, "xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sheet struct {
		Rows []struct {
			Cells []struct {
				Ref  string `xml:"r,attr"`
				Type string `xml:"t,attr"`
				V    string `xml:"v"`
				IS   struct {
					T []string `xml:"t"`
					R []struct {
						T string `xml:"t"`
					} `xml:"r"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.NewDecoder(f).Decode(&sheet); err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(sheet.Rows))
	for _, r := range sheet.Rows {
		row := Row{}
		for _, c := range r.Cells {
			col := strings.TrimRight(c.Ref, "0123456789")
			switch c.Type {
			case "s":
				var i int
				if _, err := fmt.Sscan(c.V, &i); err != nil || i >= len(ss) {
					return nil, fmt.Errorf("bad shared string index %q at %s", c.V, c.Ref)
				}
				row[col] = ss[i]
			case "inlineStr":
				var b strings.Builder
				for _, t := range c.IS.T {
					b.WriteString(t)
				}
				for _, rr := range c.IS.R {
					b.WriteString(rr.T)
				}
				row[col] = b.String()
			default:
				row[col] = c.V
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func sharedStrings(z *zip.Reader) ([]string, error) {
	f, err := open(z, "xl/sharedStrings.xml")
	if err == errNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Phonetic runs (<rPh>) hold furigana and are not part of the text.
	var sst struct {
		SI []struct {
			T []string `xml:"t"`
			R []struct {
				T string `xml:"t"`
			} `xml:"r"`
		} `xml:"si"`
	}
	if err := xml.NewDecoder(f).Decode(&sst); err != nil {
		return nil, err
	}
	out := make([]string, len(sst.SI))
	for i, si := range sst.SI {
		var b strings.Builder
		for _, t := range si.T {
			b.WriteString(t)
		}
		for _, r := range si.R {
			b.WriteString(r.T)
		}
		out[i] = b.String()
	}
	return out, nil
}

var errNotFound = fmt.Errorf("not found")

func open(z *zip.Reader, name string) (io.ReadCloser, error) {
	for _, f := range z.File {
		if f.Name == name {
			return f.Open()
		}
	}
	return nil, errNotFound
}
