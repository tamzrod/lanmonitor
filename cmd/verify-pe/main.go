// Command verify-pe checks the release executable's PE headers and icon
// resource. It is a build verification tool, not part of the application.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

const rtGroupIcon = 14

type section struct {
	virtualAddress uint32
	virtualSize    uint32
	rawSize        uint32
	rawOffset      uint32
}

type resourceEntry struct {
	nameID uint32
	data   uint32
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: verify-pe <lanmonitor-v3.exe>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	if err := verify(data); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "verify-pe:", err)
	os.Exit(1)
}

func verify(data []byte) error {
	pe, err := u32(data, 0x3c)
	if err != nil {
		return fmt.Errorf("DOS header: %w", err)
	}
	if pe+24 > uint32(len(data)) || string(data[pe:pe+4]) != "PE\x00\x00" {
		return fmt.Errorf("missing PE signature")
	}
	machine := readU16(data, pe+4)
	nsec := readU16(data, pe+6)
	optSize := readU16(data, pe+20)
	opt := pe + 24
	magic := readU16(data, opt)
	if machine != 0x8664 {
		return fmt.Errorf("machine = 0x%04x, want amd64 (0x8664)", machine)
	}
	if magic != 0x20b {
		return fmt.Errorf("optional header magic = 0x%03x, want PE32+ (0x20b)", magic)
	}
	if subsystem := readU16(data, opt+68); subsystem != 2 {
		return fmt.Errorf("subsystem = %d, want Windows GUI (2)", subsystem)
	}
	dd := opt + 112
	resRVA := readU32(data, dd+16)
	resSize := readU32(data, dd+20)
	if resRVA == 0 || resSize == 0 {
		return fmt.Errorf("no resource directory")
	}

	secs := make([]section, 0, nsec)
	for i := uint32(0); i < uint32(nsec); i++ {
		off := opt + uint32(optSize) + i*40
		if off+40 > uint32(len(data)) {
			return fmt.Errorf("truncated section table")
		}
		secs = append(secs, section{
			virtualSize:    readU32(data, off+8),
			virtualAddress: readU32(data, off+12),
			rawSize:        readU32(data, off+16),
			rawOffset:      readU32(data, off+20),
		})
	}
	resOff, ok := rvaToOffset(secs, resRVA)
	if !ok {
		return fmt.Errorf("resource RVA 0x%x is not in a section", resRVA)
	}

	types, err := resourceEntries(data, resOff, 0)
	if err != nil {
		return fmt.Errorf("resource types: %w", err)
	}
	for _, typ := range types {
		if typ.nameID != rtGroupIcon {
			continue
		}
		if typ.data&0x80000000 == 0 {
			return fmt.Errorf("group icon type is not a directory")
		}
		names, err := resourceEntries(data, resOff, typ.data&0x7fffffff)
		if err != nil {
			return fmt.Errorf("group icon names: %w", err)
		}
		for _, name := range names {
			if name.nameID != 1 {
				continue
			}
			if name.data&0x80000000 == 0 {
				return fmt.Errorf("group icon 1 is not a directory")
			}
			langs, err := resourceEntries(data, resOff, name.data&0x7fffffff)
			if err != nil {
				return fmt.Errorf("group icon languages: %w", err)
			}
			for _, lang := range langs {
				if lang.data&0x80000000 != 0 {
					continue
				}
				entry := resOff + lang.data
				if entry+16 > uint32(len(data)) {
					return fmt.Errorf("truncated resource data entry")
				}
				dataRVA := readU32(data, entry)
				dataSize := readU32(data, entry+4)
				iconOff, ok := rvaToOffset(secs, dataRVA)
				if !ok || iconOff+dataSize > uint32(len(data)) {
					return fmt.Errorf("group icon data is outside the image")
				}
				if err := checkGroupIcon(data[iconOff : iconOff+dataSize]); err != nil {
					return err
				}
				fmt.Printf("OK: PE32+ AMD64 GUI, RT_GROUP_ICON 1, %d images\n", iconCount(data[iconOff:iconOff+dataSize]))
				return nil
			}
		}
	}
	return fmt.Errorf("RT_GROUP_ICON resource 1 not found")
}

func checkGroupIcon(data []byte) error {
	if len(data) < 6 {
		return fmt.Errorf("truncated group icon")
	}
	if binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return fmt.Errorf("invalid group icon header")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count != 7 {
		return fmt.Errorf("group icon has %d images, want 7", count)
	}
	if len(data) < 6+count*14 {
		return fmt.Errorf("truncated group icon entries")
	}
	seen := map[uint16]bool{}
	for i := 0; i < count; i++ {
		off := 6 + i*14
		id := binary.LittleEndian.Uint16(data[off+12 : off+14])
		if id < 2 || id > 8 {
			return fmt.Errorf("unexpected icon resource id %d", id)
		}
		seen[id] = true
	}
	for id := uint16(2); id <= 8; id++ {
		if !seen[id] {
			return fmt.Errorf("icon resource id %d is missing", id)
		}
	}
	return nil
}

func iconCount(data []byte) int {
	if len(data) < 6 {
		return 0
	}
	return int(binary.LittleEndian.Uint16(data[4:6]))
}

func resourceEntries(data []byte, base, rel uint32) ([]resourceEntry, error) {
	off := base + rel
	if off+16 > uint32(len(data)) {
		return nil, fmt.Errorf("truncated resource directory")
	}
	named := readU16(data, off+12)
	ids := readU16(data, off+14)
	n := int(named) + int(ids)
	if off+16+uint32(n)*8 > uint32(len(data)) {
		return nil, fmt.Errorf("truncated resource entries")
	}
	out := make([]resourceEntry, 0, n)
	for i := 0; i < n; i++ {
		e := off + 16 + uint32(i)*8
		out = append(out, resourceEntry{nameID: readU32(data, e), data: readU32(data, e+4)})
	}
	return out, nil
}

func rvaToOffset(secs []section, rva uint32) (uint32, bool) {
	for _, s := range secs {
		size := s.virtualSize
		if s.rawSize > size {
			size = s.rawSize
		}
		if rva >= s.virtualAddress && rva < s.virtualAddress+size {
			return s.rawOffset + (rva - s.virtualAddress), true
		}
	}
	return 0, false
}

func readU16(data []byte, off uint32) uint16 {
	if off+2 > uint32(len(data)) {
		return 0
	}
	return binary.LittleEndian.Uint16(data[off : off+2])
}

func readU32(data []byte, off uint32) uint32 {
	if off+4 > uint32(len(data)) {
		return 0
	}
	return binary.LittleEndian.Uint32(data[off : off+4])
}

func u32(data []byte, off uint32) (uint32, error) {
	if off+4 > uint32(len(data)) {
		return 0, fmt.Errorf("offset %d is outside the file", off)
	}
	return binary.LittleEndian.Uint32(data[off : off+4]), nil
}
