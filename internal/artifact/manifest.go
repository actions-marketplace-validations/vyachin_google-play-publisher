package artifact

import (
	"archive/zip"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	// androidNamespace is the namespace of android: attributes in a compiled manifest.
	androidNamespace = "http://schemas.android.com/apk/res/android"

	// maxManifestSize bounds the decompressed base manifest. Real AAB manifests are
	// a few kilobytes; the limit keeps a malicious archive from exhausting memory.
	maxManifestSize = 1 << 20

	// manifestEntry is the protobuf XML manifest of the base module in an AAB.
	manifestEntry = "base/manifest/AndroidManifest.xml"
)

// Identity is the release identity declared by the base module of an AAB.
type Identity struct {
	ApplicationID string
	VersionCode   int64
	VersionName   string
}

// readIdentity reads and parses the base manifest of an AAB.
func readIdentity(reader *zip.Reader, path string) (Identity, error) {
	var manifest *zip.File
	for _, file := range reader.File {
		if file.Name == manifestEntry {
			manifest = file
			break
		}
	}
	if manifest == nil {
		return Identity{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage, "%s has no %s", path, manifestEntry),
			"pass a complete signed .aab built by Gradle or bundletool",
		)
	}
	if manifest.UncompressedSize64 > maxManifestSize {
		return Identity{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage, "%s: %s is larger than %d bytes", path, manifestEntry, maxManifestSize),
			"pass a valid signed .aab built by Gradle or bundletool",
		)
	}

	entry, err := manifest.Open()
	if err != nil {
		return Identity{}, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, fmt.Errorf("%s: open %s: %w", path, manifestEntry, err)),
			"pass a valid signed .aab built by Gradle or bundletool",
		)
	}
	defer func() { _ = entry.Close() }()

	data, err := io.ReadAll(io.LimitReader(entry, maxManifestSize+1))
	if err != nil {
		return Identity{}, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, fmt.Errorf("%s: read %s: %w", path, manifestEntry, err)),
			"pass a valid signed .aab built by Gradle or bundletool",
		)
	}
	if len(data) > maxManifestSize {
		return Identity{}, exitcode.WithHint(
			exitcode.Errorf(exitcode.Usage, "%s: %s is larger than %d bytes", path, manifestEntry, maxManifestSize),
			"pass a valid signed .aab built by Gradle or bundletool",
		)
	}

	return parseIdentity(path, data)
}

// parseIdentity parses the protobuf-encoded base manifest of an AAB.
//
// An AAB stores base/manifest/AndroidManifest.xml as an aapt2 proto XML
// document (aapt.pb.XmlNode), not as binary AXML. The field numbers below come
// from the AOSP frameworks/base/tools/aapt2/Resources.proto schema.
func parseIdentity(path string, data []byte) (Identity, error) {
	identity, err := parseManifest(data)
	if err != nil {
		return Identity{}, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, fmt.Errorf("%s: %w", path, err)),
			"pass a valid signed .aab built by Gradle or bundletool",
		)
	}
	return identity, nil
}

func parseManifest(data []byte) (Identity, error) {
	element, err := parseRootElement(data)
	if err != nil {
		return Identity{}, err
	}
	if element.name != "manifest" {
		return Identity{}, fmt.Errorf("root element is %q, want manifest", element.name)
	}

	var identity Identity
	seen := make(map[string]struct{}, len(element.attributes))
	versionCodeSet := false

	for _, attribute := range element.attributes {
		key := attribute.namespaceURI + "\x00" + attribute.name
		if _, ok := seen[key]; ok {
			return Identity{}, fmt.Errorf("duplicate attribute %q", attribute.name)
		}
		seen[key] = struct{}{}

		switch {
		case attribute.namespaceURI == "" && attribute.name == "package":
			if attribute.value == "" {
				return Identity{}, fmt.Errorf("manifest package is empty")
			}
			identity.ApplicationID = attribute.value
		case attribute.namespaceURI == androidNamespace && attribute.name == "versionCode":
			code, err := attribute.intValue()
			if err != nil {
				return Identity{}, fmt.Errorf("versionCode: %w", err)
			}
			if code <= 0 {
				return Identity{}, fmt.Errorf("versionCode %d is not positive", code)
			}
			identity.VersionCode = code
			versionCodeSet = true
		case attribute.namespaceURI == androidNamespace && attribute.name == "versionCodeMajor":
			major, err := attribute.intValue()
			if err != nil {
				return Identity{}, fmt.Errorf("versionCodeMajor: %w", err)
			}
			if major != 0 {
				return Identity{}, fmt.Errorf(
					"versionCodeMajor %d is not supported; the tool reads android:versionCode only", major)
			}
		case attribute.namespaceURI == androidNamespace && attribute.name == "versionName":
			name, err := attribute.versionName()
			if err != nil {
				return Identity{}, fmt.Errorf("versionName: %w", err)
			}
			identity.VersionName = name
		}
	}

	if identity.ApplicationID == "" {
		return Identity{}, fmt.Errorf("manifest has no package attribute")
	}
	if !versionCodeSet {
		return Identity{}, fmt.Errorf("manifest has no android:versionCode attribute")
	}

	return identity, nil
}

type xmlElement struct {
	name       string
	attributes []xmlAttribute
}

type xmlAttribute struct {
	namespaceURI string
	name         string
	value        string
	primitive    *int64
	stringValue  string
	isReference  bool
}

func parseRootElement(data []byte) (*xmlElement, error) {
	var element *xmlElement
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]

		if number == 1 { // XmlNode.element
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("XmlNode.element has wire type %d", wireType)
			}
			payload, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return nil, protowire.ParseError(consumed)
			}
			if element != nil {
				return nil, fmt.Errorf("manifest has more than one root element")
			}
			parsed, err := parseXMLElement(payload)
			if err != nil {
				return nil, err
			}
			element = parsed
			data = data[consumed:]
			continue
		}

		consumed, err := skipField(number, wireType, data)
		if err != nil {
			return nil, err
		}
		data = data[consumed:]
	}

	if element == nil {
		return nil, fmt.Errorf("manifest has no root element")
	}
	return element, nil
}

func parseXMLElement(data []byte) (*xmlElement, error) {
	element := &xmlElement{}
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]

		switch number {
		case 3: // XmlElement.name
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("XmlElement.name has wire type %d", wireType)
			}
			name, consumed := protowire.ConsumeString(data)
			if consumed < 0 {
				return nil, protowire.ParseError(consumed)
			}
			element.name = name
			data = data[consumed:]
		case 4: // XmlElement.attribute
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("XmlElement.attribute has wire type %d", wireType)
			}
			payload, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return nil, protowire.ParseError(consumed)
			}
			attribute, err := parseXMLAttribute(payload)
			if err != nil {
				return nil, err
			}
			element.attributes = append(element.attributes, attribute)
			data = data[consumed:]
		default:
			consumed, err := skipField(number, wireType, data)
			if err != nil {
				return nil, err
			}
			data = data[consumed:]
		}
	}
	return element, nil
}

func parseXMLAttribute(data []byte) (xmlAttribute, error) {
	var attribute xmlAttribute
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return xmlAttribute{}, protowire.ParseError(n)
		}
		data = data[n:]

		switch number {
		case 1: // namespace_uri
			if wireType != protowire.BytesType {
				return xmlAttribute{}, fmt.Errorf("XmlAttribute.namespace_uri has wire type %d", wireType)
			}
			value, consumed := protowire.ConsumeString(data)
			if consumed < 0 {
				return xmlAttribute{}, protowire.ParseError(consumed)
			}
			attribute.namespaceURI = value
			data = data[consumed:]
		case 2: // name
			if wireType != protowire.BytesType {
				return xmlAttribute{}, fmt.Errorf("XmlAttribute.name has wire type %d", wireType)
			}
			value, consumed := protowire.ConsumeString(data)
			if consumed < 0 {
				return xmlAttribute{}, protowire.ParseError(consumed)
			}
			attribute.name = value
			data = data[consumed:]
		case 3: // value
			if wireType != protowire.BytesType {
				return xmlAttribute{}, fmt.Errorf("XmlAttribute.value has wire type %d", wireType)
			}
			value, consumed := protowire.ConsumeString(data)
			if consumed < 0 {
				return xmlAttribute{}, protowire.ParseError(consumed)
			}
			attribute.value = value
			data = data[consumed:]
		case 6: // compiled_item
			if wireType != protowire.BytesType {
				return xmlAttribute{}, fmt.Errorf("XmlAttribute.compiled_item has wire type %d", wireType)
			}
			payload, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return xmlAttribute{}, protowire.ParseError(consumed)
			}
			if err := parseItem(&attribute, payload); err != nil {
				return xmlAttribute{}, err
			}
			data = data[consumed:]
		default:
			consumed, err := skipField(number, wireType, data)
			if err != nil {
				return xmlAttribute{}, err
			}
			data = data[consumed:]
		}
	}
	return attribute, nil
}

func parseItem(attribute *xmlAttribute, data []byte) error {
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]

		switch number {
		case 1: // Item.ref
			attribute.isReference = true
			consumed, err := skipField(number, wireType, data)
			if err != nil {
				return err
			}
			data = data[consumed:]
		case 2, 3, 4: // Item.str, raw_str, styled_str
			if wireType != protowire.BytesType {
				return fmt.Errorf("item string value has wire type %d", wireType)
			}
			payload, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return protowire.ParseError(consumed)
			}
			value, err := parseStringMessage(payload)
			if err != nil {
				return err
			}
			if attribute.stringValue == "" {
				attribute.stringValue = value
			}
			data = data[consumed:]
		case 7: // Item.prim
			if wireType != protowire.BytesType {
				return fmt.Errorf("item.prim has wire type %d", wireType)
			}
			payload, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return protowire.ParseError(consumed)
			}
			value, ok, err := parsePrimitive(payload)
			if err != nil {
				return err
			}
			if ok {
				attribute.primitive = &value
			}
			data = data[consumed:]
		default:
			consumed, err := skipField(number, wireType, data)
			if err != nil {
				return err
			}
			data = data[consumed:]
		}
	}
	return nil
}

func parsePrimitive(data []byte) (int64, bool, error) {
	var value int64
	var found bool
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return 0, false, protowire.ParseError(n)
		}
		data = data[n:]

		switch number {
		case 6, 7: // int_decimal_value, int_hexadecimal_value
			if wireType != protowire.VarintType {
				return 0, false, fmt.Errorf("primitive integer has wire type %d", wireType)
			}
			raw, consumed := protowire.ConsumeVarint(data)
			if consumed < 0 {
				return 0, false, protowire.ParseError(consumed)
			}
			value = int64(int32(raw))
			found = true
			data = data[consumed:]
		default:
			consumed, err := skipField(number, wireType, data)
			if err != nil {
				return 0, false, err
			}
			data = data[consumed:]
		}
	}
	return value, found, nil
}

func parseStringMessage(data []byte) (string, error) {
	var value string
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return "", protowire.ParseError(n)
		}
		data = data[n:]

		if number == 1 { // value
			if wireType != protowire.BytesType {
				return "", fmt.Errorf("string.value has wire type %d", wireType)
			}
			parsed, consumed := protowire.ConsumeString(data)
			if consumed < 0 {
				return "", protowire.ParseError(consumed)
			}
			value = parsed
			data = data[consumed:]
			continue
		}

		consumed, err := skipField(number, wireType, data)
		if err != nil {
			return "", err
		}
		data = data[consumed:]
	}
	return value, nil
}

func (a xmlAttribute) intValue() (int64, error) {
	if a.isReference {
		return 0, fmt.Errorf("uses a resource reference, which is not supported")
	}
	if a.primitive != nil {
		return *a.primitive, nil
	}
	if a.value == "" {
		return 0, fmt.Errorf("has no value")
	}
	value, err := strconv.ParseInt(strings.TrimSpace(a.value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", a.value)
	}
	return value, nil
}

func (a xmlAttribute) versionName() (string, error) {
	if a.isReference {
		return "", fmt.Errorf("uses a resource reference, which is not supported")
	}
	if a.stringValue != "" {
		return a.stringValue, nil
	}
	if a.primitive != nil {
		return "", fmt.Errorf("is an integer, want a string")
	}
	return a.value, nil
}

func skipField(number protowire.Number, wireType protowire.Type, data []byte) (int, error) {
	n := protowire.ConsumeFieldValue(number, wireType, data)
	if n < 0 {
		return 0, protowire.ParseError(n)
	}
	return n, nil
}
