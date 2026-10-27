package repository

import (
	"fmt"
	"strconv"
)

type protoMessage map[uint64][]any

func decodeProtobufIndex(data []byte) ([]rawExtension, error) {
	root, err := parseMessage(data)
	if err != nil {
		return nil, err
	}
	extensions := root[1]
	if wrapper := firstBytes(root[101]); wrapper != nil {
		message, err := parseMessage(wrapper)
		if err != nil {
			return nil, err
		}
		extensions = message[1]
	}
	result := make([]rawExtension, 0, len(extensions))
	for _, value := range extensions {
		bytes, ok := value.([]byte)
		if !ok {
			continue
		}
		message, err := parseMessage(bytes)
		if err != nil {
			return nil, fmt.Errorf("extension message: %w", err)
		}
		item := rawExtension{
			Name:        fieldString(message[1]),
			Package:     fieldString(message[2]),
			VersionCode: fieldInt(message[5]),
			Version:     fieldString(message[6]),
			NSFW:        fieldInt(message[7]) >= 2,
		}
		if resources := firstBytes(message[3]); resources != nil {
			resourceMessage, err := parseMessage(resources)
			if err != nil {
				return nil, fmt.Errorf("resource message: %w", err)
			}
			item.APKURL = fieldString(resourceMessage[1])
			item.IconURL = fieldString(resourceMessage[2])
		}
		for _, sourceValue := range message[8] {
			sourceBytes, ok := sourceValue.([]byte)
			if !ok {
				continue
			}
			sourceMessage, err := parseMessage(sourceBytes)
			if err != nil {
				return nil, fmt.Errorf("source message: %w", err)
			}
			item.Sources = append(item.Sources, rawSource{
				ID:       strconv.FormatUint(fieldInt(sourceMessage[1]), 10),
				Name:     fieldString(sourceMessage[2]),
				Language: fieldString(sourceMessage[3]),
				BaseURL:  fieldString(sourceMessage[4]),
			})
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no extensions found")
	}
	return result, nil
}

func parseMessage(data []byte) (protoMessage, error) {
	message := protoMessage{}
	for position := 0; position < len(data); {
		key, read, err := readVarint(data[position:])
		if err != nil {
			return nil, err
		}
		position += read
		field, wire := key>>3, key&7
		switch wire {
		case 0:
			value, read, err := readVarint(data[position:])
			if err != nil {
				return nil, err
			}
			position += read
			message[field] = append(message[field], value)
		case 1:
			position += 8
		case 2:
			length, read, err := readVarint(data[position:])
			if err != nil {
				return nil, err
			}
			position += read
			if length > uint64(len(data)-position) {
				return nil, fmt.Errorf("invalid field %d length %d", field, length)
			}
			value := append([]byte(nil), data[position:position+int(length)]...)
			position += int(length)
			message[field] = append(message[field], value)
		case 5:
			position += 4
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", wire)
		}
		if position > len(data) {
			return nil, fmt.Errorf("truncated protobuf message")
		}
	}
	return message, nil
}

func readVarint(data []byte) (uint64, int, error) {
	var result uint64
	for i, value := range data {
		if i == 10 {
			break
		}
		result |= uint64(value&0x7f) << (7 * i)
		if value&0x80 == 0 {
			return result, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("invalid protobuf varint")
}

func firstBytes(values []any) []byte {
	if len(values) == 0 {
		return nil
	}
	value, _ := values[0].([]byte)
	return value
}

func fieldString(values []any) string { return string(firstBytes(values)) }

func fieldInt(values []any) uint64 {
	if len(values) == 0 {
		return 0
	}
	value, _ := values[0].(uint64)
	return value
}
