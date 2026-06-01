package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	for {
		body, format, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			resp := fail(nil, -32700, "parse error: "+err.Error())
			if err := writeMessage(output, resp, format); err != nil {
				return err
			}
			continue
		}

		resp := s.Handle(ctx, req)
		if resp.JSONRPC == "" && resp.ID == nil {
			continue
		}
		if err := writeMessage(output, resp, format); err != nil {
			return err
		}
	}
}

type messageFormat int

const (
	messageFormatFramed messageFormat = iota
	messageFormatJSONLine
)

func readMessage(reader *bufio.Reader) ([]byte, messageFormat, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, messageFormatFramed, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			return []byte(strings.TrimSpace(line)), messageFormatJSONLine, nil
		}
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, messageFormatFramed, fmt.Errorf("invalid Content-Length: %w", err)
			}
			contentLength = parsed
		}
	}
	if contentLength < 0 {
		return nil, messageFormatFramed, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	return body, messageFormatFramed, err
}

func writeMessage(output io.Writer, value any, format messageFormat) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if format == messageFormatJSONLine {
		_, err = io.Copy(output, bytes.NewReader(append(body, '\n')))
		return err
	}
	header := []byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body)))
	_, err = io.Copy(output, bytes.NewReader(append(header, body...)))
	return err
}
