// Package artifacttest builds small AAB fixtures for tests.
package artifacttest

import (
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"
)

const androidNamespace = "http://schemas.android.com/apk/res/android"

// Manifest returns a protobuf-encoded base AndroidManifest.xml with the given
// package, version code and version name. An empty versionName omits the
// attribute.
func Manifest(applicationID string, versionCode int64, versionName string) []byte {
	var element []byte
	element = appendStringField(element, 3, "manifest") // XmlElement.name
	element = appendBytesField(element, 4, attribute("", "package", applicationID, nil))
	element = appendBytesField(element, 4, attribute(androidNamespace, "versionCode",
		strconv.FormatInt(versionCode, 10), itemInt(versionCode)))
	if versionName != "" {
		element = appendBytesField(element, 4, attribute(androidNamespace, "versionName",
			versionName, itemString(versionName)))
	}

	var node []byte
	node = appendBytesField(node, 1, element) // XmlNode.element
	return node
}

func attribute(namespaceURI, name, value string, compiledItem []byte) []byte {
	var attribute []byte
	if namespaceURI != "" {
		attribute = appendStringField(attribute, 1, namespaceURI) // namespace_uri
	}
	attribute = appendStringField(attribute, 2, name)  // name
	attribute = appendStringField(attribute, 3, value) // value
	if compiledItem != nil {
		attribute = appendBytesField(attribute, 6, compiledItem) // compiled_item
	}
	return attribute
}

func itemInt(value int64) []byte {
	var primitive []byte
	primitive = appendVarintField(primitive, 6, uint64(value)) // Primitive.int_decimal_value

	var item []byte
	item = appendBytesField(item, 7, primitive) // Item.prim
	return item
}

func itemString(value string) []byte {
	var str []byte
	str = appendStringField(str, 1, value) // String.value

	var item []byte
	item = appendBytesField(item, 2, str) // Item.str
	return item
}

func appendStringField(dst []byte, number protowire.Number, value string) []byte {
	dst = protowire.AppendTag(dst, number, protowire.BytesType)
	return protowire.AppendString(dst, value)
}

func appendBytesField(dst []byte, number protowire.Number, value []byte) []byte {
	dst = protowire.AppendTag(dst, number, protowire.BytesType)
	return protowire.AppendBytes(dst, value)
}

func appendVarintField(dst []byte, number protowire.Number, value uint64) []byte {
	dst = protowire.AppendTag(dst, number, protowire.VarintType)
	return protowire.AppendVarint(dst, value)
}
