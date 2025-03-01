/*
   Copyright © 2022 The CDI Authors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package schema

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/container-orchestrated-devices/container-device-interface/internal/multierror"
	"github.com/pkg/errors"
	schema "github.com/xeipuuv/gojsonschema"
)

const (
	// BuiltinSchemaName names the builtin schema for Load()/Set().
	BuiltinSchemaName = "builtin"
	// NoneSchemaName names the NOP-schema for Load()/Set().
	NoneSchemaName = "none"
	// builtinSchemaFile is the builtin schema URI in our embedded FS.
	builtinSchemaFile = "file:///schema.json"
)

// Schema is a JSON validation schema.
type Schema struct {
	schema *schema.Schema
}

// Error wraps a JSON validation result.
type Error struct {
	Result *schema.Result
}

// Set sets the default validating JSON schema.
func Set(s *Schema) {
	current = s
}

// Get returns the active validating JSON schema.
func Get() *Schema {
	return current
}

// BuiltinSchema returns the builtin schema if we have a valid one. Otherwise
// it falls back to NopSchema().
func BuiltinSchema() *Schema {
	if builtin != nil {
		return builtin
	}

	s, err := schema.NewSchema(
		schema.NewReferenceLoaderFileSystem(
			builtinSchemaFile,
			http.FS(builtinFS),
		),
	)

	if err == nil {
		builtin = &Schema{schema: s}
	} else {
		builtin = NopSchema()
	}

	return builtin
}

// NopSchema returns an validating JSON Schema that does no real validation.
func NopSchema() *Schema {
	return &Schema{}
}

// ReadAndValidate all data from the given reader, using the active schema for validation.
func ReadAndValidate(r io.Reader) ([]byte, error) {
	return current.ReadAndValidate(r)
}

// Validate validates the data read from an io.Reader against the active schema.
func Validate(r io.Reader) error {
	return current.Validate(r)
}

// ValidateData validates the given JSON document against the active schema.
func ValidateData(data []byte) error {
	return current.ValidateData(data)
}

// ValidateFile validates the given JSON file against the active schema.
func ValidateFile(path string) error {
	return current.ValidateFile(path)
}

// ValidateType validates a go object against the schema.
func ValidateType(obj interface{}) error {
	return current.ValidateType(obj)
}

// Load the given JSON Schema.
func Load(source string) (*Schema, error) {
	var (
		loader schema.JSONLoader
		err    error
		s      *schema.Schema
	)

	source = strings.TrimSpace(source)

	switch {
	case source == BuiltinSchemaName:
		return BuiltinSchema(), nil
	case source == NoneSchemaName, source == "":
		return NopSchema(), nil
	case strings.HasPrefix(source, "file://"):
	case strings.HasPrefix(source, "http://"):
	case strings.HasPrefix(source, "https://"):
	default:
		if strings.Index(source, "://") < 0 {
			source, err = filepath.Abs(source)
			if err != nil {
				return nil, errors.Wrapf(err,
					"failed to get JSON schema absolute path for %s", source)
			}
			source = "file://" + source
		}
	}

	loader = schema.NewReferenceLoader(source)

	s, err = schema.NewSchema(loader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load JSON schema")
	}

	return &Schema{schema: s}, nil
}

// ReadAndValidate all data from the given reader, using the schema for validation.
func (s *Schema) ReadAndValidate(r io.Reader) ([]byte, error) {
	loader, reader := schema.NewReaderLoader(r)
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read data for validation")
	}
	return data, s.validate(loader)
}

// Validate validates the data read from an io.Reader against the schema.
func (s *Schema) Validate(r io.Reader) error {
	_, err := s.ReadAndValidate(r)
	return err
}

// ValidateData validates the given JSON data against the schema.
func (s *Schema) ValidateData(data []byte) error {
	var (
		any interface{}
		err error
	)

	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte{'{'}) {
		err = yaml.Unmarshal(data, &any)
		if err != nil {
			return errors.Wrap(err, "failed to YAML unmarshal data for validation")
		}
		data, err = json.Marshal(any)
		if err != nil {
			return errors.Wrap(err, "failed to JSON remarshal data for validation")
		}
	}

	return s.validate(schema.NewBytesLoader(data))
}

// ValidateFile validates the given JSON file against the schema.
func (s *Schema) ValidateFile(path string) error {
	if filepath.Ext(path) == ".json" {
		return s.validate(schema.NewReferenceLoader("file://" + path))
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	return s.ValidateData(data)
}

// ValidateType validates a go object against the schema.
func (s *Schema) ValidateType(obj interface{}) error {
	l := schema.NewGoLoader(obj)
	return s.validate(l)
}

// Validate the (to be) loaded doc against the schema.
func (s *Schema) validate(doc schema.JSONLoader) error {
	if s == nil || s.schema == nil {
		return nil
	}

	docErr, jsonErr := s.schema.Validate(doc)
	if jsonErr != nil {
		return errors.Wrap(jsonErr, "failed to load JSON data for validation")
	}
	if docErr.Valid() {
		return nil
	}

	return &Error{Result: docErr}
}

// Error returns the given Result's errors as a single error string.
func (e *Error) Error() string {
	if e == nil || e.Result == nil || e.Result.Valid() {
		return ""
	}

	var multi error
	for _, err := range e.Result.Errors() {
		multi = multierror.Append(multi, fmt.Errorf("%v", err))
	}
	return multi.Error()
}

var (
	// our builtin schema
	builtin *Schema
	// currently loaded schema, builtin by default
	current = BuiltinSchema()
)

//go:embed *.json
var builtinFS embed.FS
