package file

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	autoctx "github.com/vladimirvivien/automi/api/context"
)

// CsvSource implements an Source process that reads the content of a
// specified io.Reader or a os.File and emits its record via its Output Channel
// and serializes each row as a slice []string.
type CsvSource struct {
	filepath    string   // path for the file
	delimChar   rune     // Delimiter charater, defaults to comma
	commentChar rune     // Charater indicating line is a cg.org/omment
	headers     []string // Column header names (specified here or read from file)
	hasHeaders  bool     // indicates first row is for headers (default false).
	fieldCount  int      // if greater than zero is used to validate field count

	file      *os.File
	srcReader io.Reader
	csvReader *csv.Reader
	log       *logrus.Entry
	output    chan interface{}
}

func New() *CsvSource {
	csv := &CsvSource{
		delimChar:   ',',
		commentChar: '#',
		output:      make(chan interface{}, 1024),
	}
	return csv
}

func (c *CsvSource) WithReader(reader io.Reader) *CsvSource {
	c.srcReader = reader
	return c
}

func (c *CsvSource) WithFile(path string) *CsvSource {
	c.filepath = path
	return c
}

func (c *CsvSource) DelimChar(char rune) *CsvSource {
	c.delimChar = char
	return c
}

func (c *CsvSource) CommentChar(char rune) *CsvSource {
	c.commentChar = char
	return c
}

func (c *CsvSource) HasHeaders() *CsvSource {
	c.hasHeaders = true
	return c
}

func (c *CsvSource) init(ctx context.Context) error {
	// extract logger
	log, ok := autoctx.GetLogEntry(ctx)
	if !ok {
		log = logrus.WithField("Component", "CsvSource")
		log.Error("No logger found incontext")
	}
	c.log = log.WithFields(logrus.Fields{
		"Component": "CsvSource",
		"Type":      fmt.Sprintf("%T", c),
	})

	// establish defaults
	if c.delimChar == 0 {
		c.delimChar = ','
	}

	if c.commentChar == 0 {
		c.commentChar = '#'
	}

	var reader io.Reader

	if c.srcReader != nil {
		reader = c.srcReader
		c.log.Info("Using IO Reader")
	} else {
		// open file
		file, err := os.Open(c.filepath)
		if err != nil {
			return fmt.Errorf("Failed to open file: %s ", err)
		}
		reader = file
		c.file = file
		c.log.Info("Using source file ", file.Name())
	}

	c.csvReader = csv.NewReader(reader)
	c.csvReader.Comment = c.commentChar
	c.csvReader.Comma = c.delimChar
	c.csvReader.TrimLeadingSpace = true
	c.csvReader.LazyQuotes = true

	// resolve header and field count
	if c.hasHeaders {
		if headers, err := c.csvReader.Read(); err == nil {
			c.fieldCount = len(headers)
			c.headers = headers
		} else {
			return fmt.Errorf("Unable to read header row: %s", err)
		}
	} else {
		if c.headers != nil {
			c.fieldCount = len(c.headers)
		}
	}
	log.Debug("hasHeaders ", c.hasHeaders, " headers [", c.headers, "]")
	c.log.Info("Source initializede")

	return nil
}

func (c *CsvSource) GetOutput() <-chan interface{} {
	return c.output
}

func (c *CsvSource) Open(ctx context.Context) (err error) {
	if err = c.init(ctx); err != nil {
		return
	}

	c.log.Info("Source opened")

	go func() {
		defer func() {
			close(c.output)
			if c.file != nil {
				err = c.file.Close()
				if err != nil {
					c.log.Error(err)
				}
			}
			c.log.Info("Source closed")
		}()

		for {
			row, err := c.csvReader.Read()
			if err != nil {
				if err == io.EOF {
					return
				}
				c.log.Error(fmt.Errorf("Error reading row: %s", err))
				continue
			}

			select {
			case c.output <- row:
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	return nil
}
