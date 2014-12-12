package binder

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrorEmptyContentType       = NewError([]string{}, ContentTypeError, "Empty Content-Type")
	ErrorUnsupportedContentType = NewError([]string{}, ContentTypeError, "Unsupported Content-Type")
	ErrorInputNotByReference    = NewError([]string{}, DeserializationError, "input binding model is not by reference")
	ErrorInputIsNotStructure    = NewError([]string{}, DeserializationError, "binding model is required to be structure")
)

func Bind(obj interface{}, req *http.Request) Errors {
	contentType := req.Header.Get("Content-Type")
	if req.Method == "POST" || req.Method == "PUT" || contentType != "" {
		if strings.Contains(contentType, "form-urlencoded") {
			return Form(obj, req)
		} else if strings.Contains(contentType, "multipart/form-data") {
			return MultipartForm(obj, req)
		} else if strings.Contains(contentType, "json") {
			return Json(obj, req)
		} else {
			var errors Errors
			if contentType == "" {
				errors.AddError(ErrorEmptyContentType)
			} else {
				errors.AddError(ErrorUnsupportedContentType)
			}
			return errors
		}
	} else {
		return Form(obj, req)
	}
}

// Form is middleware to deserialize form-urlencoded data from the request.
// It gets data from the form-urlencoded body, if present, or from the
// query string. It uses the http.Request.ParseForm() method
// to perform deserialization, then reflection is used to map each field
// into the struct with the proper type. Structs with primitive slice types
// (bool, float, int, string) can support deserialization of repeated form
// keys, for example: key=val1&key=val2&key=val3
// An interface pointer can be added as a second argument in order
// to map the struct to a specific interface.
func Form(formStruct interface{}, req *http.Request) Errors {
	var bindErrors Errors

	v := reflect.ValueOf(formStruct)
	if v.Kind() != reflect.Ptr {
		return append(bindErrors, ErrorInputNotByReference)
	}

	//reset element to zero variant
	v = v.Elem()
	if v.Kind() == reflect.Ptr && v.CanSet() && v.IsNil() {
		v.Set(reflect.New(v.Type().Elem()))
	}

	v = reflect.Indirect(v)
	if v.Kind() != reflect.Struct || !v.CanSet() {
		return append(bindErrors, ErrorInputIsNotStructure)
	}

	// Format validation of the request body or the URL would add considerable overhead,
	// and ParseForm does not complain when URL encoding is off.
	// Because an empty request body or url can also mean absence of all needed values,
	// it is not in all cases a bad request, so let's return 422.
	parseErr := req.ParseForm()
	if parseErr != nil {
		bindErrors.Add([]string{}, DeserializationError, parseErr.Error())
	}
	mapForm(v, req.Form, nil, bindErrors)
	validateErrs := validate(v.Interface(), req)
	if validateErrs != nil {
		bindErrors = append(bindErrors, validateErrs...)
	}
	return bindErrors
}

// MultipartForm works much like Form, except it can parse multipart forms
// and handle file uploads. Like the other deserialization middleware handlers,
// you can pass in an interface to make the interface available for injection
// into other handlers later.
func MultipartForm(formStruct interface{}, req *http.Request) Errors {
	var bindErrors Errors

	v := reflect.ValueOf(formStruct)
	if v.Kind() != reflect.Ptr {
		return append(bindErrors, ErrorInputNotByReference)
	}

	//reset element to zero variant
	v = v.Elem()
	if v.Kind() == reflect.Ptr && v.CanSet() && v.IsNil() {
		v.Set(reflect.New(v.Type().Elem()))
	}

	v = reflect.Indirect(v)
	if v.Kind() != reflect.Struct || !v.CanSet() {
		return append(bindErrors, ErrorInputIsNotStructure)
	}

	// This if check is necessary due to https://github.com/martini-contrib/csrf/issues/6
	if req.MultipartForm == nil {
		// Workaround for multipart forms returning nil instead of an error
		// when content is not multipart; see https://code.google.com/p/go/issues/detail?id=6334
		if multipartReader, err := req.MultipartReader(); err != nil {
			// TODO: Cover this and the next error check with tests
			bindErrors.Add([]string{}, DeserializationError, err.Error())
		} else {
			form, parseErr := multipartReader.ReadForm(MaxMemory)
			if parseErr != nil {
				bindErrors.Add([]string{}, DeserializationError, parseErr.Error())
			}
			req.MultipartForm = form
		}
	}

	mapForm(v, req.MultipartForm.Value, req.MultipartForm.File, bindErrors)
	validateErrs := validate(v.Interface(), req)
	if validateErrs != nil {
		return append(bindErrors, validateErrs...)
	}
	return bindErrors
}

// Json is middleware to deserialize a JSON payload from the request
// into the struct that is passed in. The resulting struct is then
// validated, but no error handling is actually performed here.
// An interface pointer can be added as a second argument in order
// to map the struct to a specific interface.
func Json(jsonStruct interface{}, req *http.Request) Errors {
	var bindErrors Errors

	v := reflect.ValueOf(jsonStruct)
	if v.Kind() != reflect.Ptr {
		return append(bindErrors, ErrorInputNotByReference)
	}

	//reset element to zero variant
	v = v.Elem()
	if v.Kind() == reflect.Ptr && v.CanSet() && v.IsNil() {
		v.Set(reflect.New(v.Type().Elem()))
	}

	v = reflect.Indirect(v)
	if v.Kind() != reflect.Struct || !v.CanSet() {
		return append(bindErrors, ErrorInputIsNotStructure)
	}

	if req.Body != nil {
		defer req.Body.Close()
		err := json.NewDecoder(req.Body).Decode(jsonStruct)
		if err != nil && err != io.EOF {
			bindErrors.Add([]string{}, DeserializationError, err.Error())
		}
	}

	validateErrs := validate(jsonStruct, req)
	if validateErrs != nil {
		return append(bindErrors, validateErrs...)
	}
	return bindErrors
}

var (
	alphaDashPattern    = regexp.MustCompile("[^\\d\\w-_]")
	alphaDashDotPattern = regexp.MustCompile("[^\\d\\w-_\\.]")
	emailPattern        = regexp.MustCompile("[\\w!#$%&'*+/=?^_`{|}~-]+(?:\\.[\\w!#$%&'*+/=?^_`{|}~-]+)*@(?:[\\w](?:[\\w-]*[\\w])?\\.)+[a-zA-Z0-9](?:[\\w-]*[\\w])?")
	urlPattern          = regexp.MustCompile(`(http|https):\/\/[\w\-_]+(\.[\w\-_]+)+([\w\-\.,@?^=%&amp;:/~\+#]*[\w\-\@?^=%&amp;/~\+#])?`)
)

// Performs required field checking on a struct
func validateStruct(errors Errors, obj interface{}) Errors {
	typ := reflect.TypeOf(obj)
	val := reflect.ValueOf(obj)

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		val = val.Elem()
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Allow ignored fields in the struct
		if field.Tag.Get("form") == "-" || !val.Field(i).CanInterface() {
			continue
		}

		fieldValue := val.Field(i).Interface()
		zero := reflect.Zero(field.Type).Interface()

		// Validate nested and embedded structs (if pointer, only do so if not nil)
		if field.Type.Kind() == reflect.Struct ||
			(field.Type.Kind() == reflect.Ptr && !reflect.DeepEqual(zero, fieldValue) &&
				field.Type.Elem().Kind() == reflect.Struct) {
			errors = validateStruct(errors, fieldValue)
		}

		// Match rules.
		for _, rule := range strings.Split(field.Tag.Get("binding"), ";") {
			if len(rule) == 0 {
				continue
			}

			switch {
			case rule == "Required":
				if reflect.DeepEqual(zero, fieldValue) {
					errors.Add([]string{field.Name}, RequiredError, "Required")
					break
				}
			case rule == "AlphaDash":
				if alphaDashPattern.MatchString(fmt.Sprintf("%v", fieldValue)) {
					errors.Add([]string{field.Name}, AlphaDashError, "AlphaDash")
					break
				}
			case rule == "AlphaDashDot":
				if alphaDashDotPattern.MatchString(fmt.Sprintf("%v", fieldValue)) {
					errors.Add([]string{field.Name}, AlphaDashDotError, "AlphaDashDot")
					break
				}
			case strings.HasPrefix(rule, "MinSize("):
				min, _ := strconv.Atoi(rule[8 : len(rule)-1])
				if str, ok := fieldValue.(string); ok && utf8.RuneCountInString(str) < min {
					errors.Add([]string{field.Name}, MinSizeError, "MinSize")
					break
				}
				v := reflect.ValueOf(fieldValue)
				if v.Kind() == reflect.Slice && v.Len() < min {
					errors.Add([]string{field.Name}, MinSizeError, "MinSize")
					break
				}
			case strings.HasPrefix(rule, "MaxSize("):
				max, _ := strconv.Atoi(rule[8 : len(rule)-1])
				if str, ok := fieldValue.(string); ok && utf8.RuneCountInString(str) > max {
					errors.Add([]string{field.Name}, MaxSizeError, "MaxSize")
					break
				}
				v := reflect.ValueOf(fieldValue)
				if v.Kind() == reflect.Slice && v.Len() > max {
					errors.Add([]string{field.Name}, MaxSizeError, "MaxSize")
					break
				}
			case rule == "Email":
				if !emailPattern.MatchString(fmt.Sprintf("%v", fieldValue)) {
					errors.Add([]string{field.Name}, EmailError, "Email")
					break
				}
			case rule == "Url":
				str := fmt.Sprintf("%v", fieldValue)
				if len(str) == 0 {
					continue
				} else if !urlPattern.MatchString(str) {
					errors.Add([]string{field.Name}, UrlError, "Url")
					break
				}
			}
		}
	}
	return errors
}

// Takes values from the form data and puts them into a struct
func mapForm(formStruct reflect.Value, form map[string][]string,
	formfile map[string][]*multipart.FileHeader, errors Errors) {
	formStruct = reflect.Indirect(formStruct)
	typ := formStruct.Type()

	for i := 0; i < typ.NumField(); i++ {
		typeField := typ.Field(i)
		structField := formStruct.Field(i)

		if typeField.Type.Kind() == reflect.Ptr && typeField.Anonymous {
			structField.Set(reflect.New(typeField.Type.Elem()))
			mapForm(structField.Elem(), form, formfile, errors)
			if reflect.DeepEqual(structField.Elem().Interface(), reflect.Zero(structField.Elem().Type()).Interface()) {
				structField.Set(reflect.Zero(structField.Type()))
			}
		} else if typeField.Type.Kind() == reflect.Struct {
			mapForm(structField, form, formfile, errors)
		} else if inputFieldName := typeField.Tag.Get("form"); inputFieldName != "" {
			if !structField.CanSet() {
				continue
			}

			inputValue, exists := form[inputFieldName]
			if exists {
				numElems := len(inputValue)
				if structField.Kind() == reflect.Slice && numElems > 0 {
					sliceOf := structField.Type().Elem().Kind()
					slice := reflect.MakeSlice(structField.Type(), numElems, numElems)
					for i := 0; i < numElems; i++ {
						setWithProperType(sliceOf, inputValue[i], slice.Index(i), inputFieldName, errors)
					}
					formStruct.Field(i).Set(slice)
				} else {
					setWithProperType(typeField.Type.Kind(), inputValue[0], structField, inputFieldName, errors)
				}
				continue
			}

			inputFile, exists := formfile[inputFieldName]
			if !exists {
				continue
			}
			fhType := reflect.TypeOf((*multipart.FileHeader)(nil))
			numElems := len(inputFile)
			if structField.Kind() == reflect.Slice && numElems > 0 && structField.Type().Elem() == fhType {
				slice := reflect.MakeSlice(structField.Type(), numElems, numElems)
				for i := 0; i < numElems; i++ {
					slice.Index(i).Set(reflect.ValueOf(inputFile[i]))
				}
				structField.Set(slice)
			} else if structField.Type() == fhType {
				structField.Set(reflect.ValueOf(inputFile[0]))
			}
		}
	}
}

// This sets the value in a struct of an indeterminate type to the
// matching value from the request (via Form middleware) in the
// same type, so that not all deserialized values have to be strings.
// Supported types are string, int, float, and bool.
func setWithProperType(valueKind reflect.Kind, val string, structField reflect.Value, nameInTag string, errors Errors) {
	switch valueKind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val == "" {
			val = "0"
		}
		intVal, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			errors.Add([]string{nameInTag}, IntegerTypeError, "Value could not be parsed as integer")
		} else {
			structField.SetInt(intVal)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if val == "" {
			val = "0"
		}
		uintVal, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			errors.Add([]string{nameInTag}, IntegerTypeError, "Value could not be parsed as unsigned integer")
		} else {
			structField.SetUint(uintVal)
		}
	case reflect.Bool:
		if val == "on" {
			structField.SetBool(true)
			return
		}

		if val == "" {
			val = "false"
		}
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			errors.Add([]string{nameInTag}, BooleanTypeError, "Value could not be parsed as boolean")
		} else if boolVal {
			structField.SetBool(true)
		}
	case reflect.Float32:
		if val == "" {
			val = "0.0"
		}
		floatVal, err := strconv.ParseFloat(val, 32)
		if err != nil {
			errors.Add([]string{nameInTag}, FloatTypeError, "Value could not be parsed as 32-bit float")
		} else {
			structField.SetFloat(floatVal)
		}
	case reflect.Float64:
		if val == "" {
			val = "0.0"
		}
		floatVal, err := strconv.ParseFloat(val, 64)
		if err != nil {
			errors.Add([]string{nameInTag}, FloatTypeError, "Value could not be parsed as 64-bit float")
		} else {
			structField.SetFloat(floatVal)
		}
	case reflect.String:
		structField.SetString(val)
	}
}

// validate by the build in validation rules and tries to run the model ValidateBinder function if set
func validate(obj interface{}, req *http.Request) Errors {
	var bindErrors Errors
	v := reflect.ValueOf(obj)
	k := v.Kind()
	if k == reflect.Interface || k == reflect.Ptr {
		v = v.Elem()
		k = v.Kind()
	}

	if k == reflect.Slice || k == reflect.Array {
		for i := 0; i < v.Len(); i++ {
			e := v.Index(i).Interface()
			bindErrors = validateStruct(bindErrors, e)
			if validator, ok := e.(Validator); ok {
				bindErrors = validator.ValidateBinder(req, bindErrors)
			}
		}
	} else {
		bindErrors = validateStruct(bindErrors, obj)
		if validator, ok := obj.(Validator); ok {
			bindErrors = validator.ValidateBinder(req, bindErrors)
		}
	}
	return bindErrors
}

type (
	// Implement the Validator interface to handle some rudimentary
	// request validation logic so your application doesn't have to.
	Validator interface {
		// ValidateBinder validates that the request is OK. It is recommended
		// that validation be limited to checking values for syntax and
		// semantics, enough to know that you can make sense of the request
		// in your application. For example, you might verify that a credit
		// card number matches a valid pattern, but you probably wouldn't
		// perform an actual credit card authorization here.
		ValidateBinder(*http.Request, Errors) Errors
	}
)

var (
	// Maximum amount of memory to use when parsing a multipart form.
	// Set this to whatever value you prefer; default is 16 MB.
	MaxMemory = int64(1024 * 1024 * 16)
)

const (
	jsonContentType = "application/json; charset=utf-8"
)