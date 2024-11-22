package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test1(t *testing.T) {
	miscDirs := []string{`../../../github.com/admpub/nging/template/...`}
	var prefixes []string
	prefixes, miscDirs = buildGoGenerateCommandData(``, miscDirs)
	b, _ := json.MarshalIndent(miscDirs, ``, `  `)
	fmt.Println(string(b))
	b, _ = json.MarshalIndent(prefixes, ``, `  `)
	fmt.Println(string(b))
	assert.Equal(t, `../../../github.com/admpub/nging/template/...`, miscDirs[0])
	assert.Equal(t, `../../../github.com/admpub/nging/`, prefixes[0])
}
