package content

import (
	"context"
	"testing"
	"testing/fstest"
)

// TestDetectLicenseID exercises the marker-phrase matcher against
// distinctive sentences from each recognised license. Keeping the
// test inputs short (rather than the full boilerplate) keeps the
// table compact AND verifies the matcher works on truncated input —
// which is what happens in practice when the 16 KiB read cap fires
// on a long license.
func TestDetectLicenseID(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"MIT",
			"MIT License\n\nCopyright (c) 2026 ...\n\nPermission is hereby granted, free of charge, to any person...",
			"MIT",
		},
		{
			"Apache-2.0",
			"                                 Apache License\n                           Version 2.0, January 2004",
			"Apache-2.0",
		},
		{
			"BSD-3-Clause",
			"BSD 3-Clause License\n\nRedistribution and use in source and binary forms, with or without modification, are permitted...\n\n3. Neither the name of the copyright holder nor the names of its contributors may be used...",
			"BSD-3-Clause",
		},
		{
			"BSD-2-Clause",
			"BSD 2-Clause License\n\nRedistribution and use in source and binary forms, with or without modification, are permitted...",
			"BSD-2-Clause",
		},
		{
			"ISC",
			"ISC License\n\nPermission to use, copy, modify, and/or distribute this software for any purpose with or without fee...",
			"ISC",
		},
		{
			"GPL-3.0",
			"                    GNU GENERAL PUBLIC LICENSE\n                       Version 3, 29 June 2007",
			"GPL-3.0",
		},
		{
			"GPL-2.0",
			"                    GNU GENERAL PUBLIC LICENSE\n                       Version 2, June 1991",
			"GPL-2.0",
		},
		{
			"LGPL-3.0",
			"                   GNU LESSER GENERAL PUBLIC LICENSE\n                       Version 3, 29 June 2007",
			"LGPL-3.0",
		},
		{
			"AGPL-3.0",
			"                    GNU AFFERO GENERAL PUBLIC LICENSE\n                       Version 3, 19 November 2007",
			"AGPL-3.0",
		},
		{
			"MPL-2.0",
			"Mozilla Public License Version 2.0\n==================================",
			"MPL-2.0",
		},
		{
			"Unlicense",
			"This is free and unencumbered software released into the public domain.",
			"Unlicense",
		},
		{
			"BSL-1.0",
			"Boost Software License - Version 1.0 - August 17th, 2003",
			"BSL-1.0",
		},
		{
			"CC0-1.0",
			"Creative Commons CC0 1.0 Universal",
			"CC0-1.0",
		},
		{"empty body", "", ""},
		{"unrelated text", "This is just some random README content.", ""},
		{
			"Apache cited inside MIT preamble (precedence test)",
			"MIT License\n\nThis project draws on Apache License Version 2.0 work...\n\nPermission is hereby granted, free of charge, to any person...",
			// First-match-wins → Apache fires before MIT in our switch
			// because the Apache marker is more specific. Document the
			// behavior; if a real project does this we'll need to
			// distinguish boilerplate from prose.
			"Apache-2.0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectLicenseID(c.body)
			if got != c.want {
				t.Errorf("detectLicenseID(...) = %q, want %q", got, c.want)
			}
		})
	}
}

// TestLicenseAttributes exercises the full pipeline: filename match →
// Detect → Attributes returns license_id from the body.
func TestLicenseAttributes(t *testing.T) {
	cases := []struct {
		filename string
		body     string
		wantID   string
	}{
		{
			"LICENSE",
			"MIT License\n\nCopyright (c) 2026 file-search-on\n\nPermission is hereby granted, free of charge...",
			"MIT",
		},
		{
			"LICENCE", // British spelling
			"Apache License\n                           Version 2.0",
			"Apache-2.0",
		},
		{
			"COPYING",
			"                    GNU GENERAL PUBLIC LICENSE\n                       Version 3, 29 June 2007",
			"GPL-3.0",
		},
		{
			"UNLICENSE",
			"This is free and unencumbered software released into the public domain.",
			"Unlicense",
		},
	}
	for _, c := range cases {
		t.Run(c.filename, func(t *testing.T) {
			fsys := fstest.MapFS{c.filename: &fstest.MapFile{Data: []byte(c.body)}}
			ct := DefaultRegistry().Detect(fsys, c.filename)
			if ct == nil || ct.Name() != "repo/license" {
				t.Fatalf("Detect: got %v, want repo/license", ct)
			}
			attrs, err := ct.Attributes(context.Background(), fsys, c.filename)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if got := attrs["license_id"]; got != c.wantID {
				t.Errorf("license_id = %v, want %q", got, c.wantID)
			}
		})
	}
}
