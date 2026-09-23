package config

import (
	"reflect"
	"strings"

	"github.com/spf13/viper"
)

// BindEnvKeys registers every leaf key of schema (a struct tagged with `mapstructure`) with v so
// that AutomaticEnv resolves its environment variable even when the configuration file omits the
// key. Viper only consults the environment for keys it already knows from the file, defaults, or
// an explicit binding; without this, a commented-out sample key such as login.provider would make
// GATEWAY_LOGIN_PROVIDER silently ineffective. Slices and maps cannot be set from a single
// variable and are skipped.
func BindEnvKeys(v *viper.Viper, schema any) error {
	return bindEnvKeys(v, reflect.TypeOf(schema), "")
}

func bindEnvKeys(v *viper.Viper, t reflect.Type, prefix string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("mapstructure"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		ft := field.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch {
		case ft.Kind() == reflect.Struct && ft.PkgPath()+"."+ft.Name() != "time.Time":
			if e := bindEnvKeys(v, ft, key); e != nil {
				return e
			}
		case ft.Kind() == reflect.Slice || ft.Kind() == reflect.Map:
			continue
		default:
			if e := v.BindEnv(key); e != nil {
				return e
			}
		}
	}
	return nil
}
