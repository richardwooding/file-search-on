package content

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractPerlSymbols_SimpleModule(t *testing.T) {
	src := []byte(`package Acme::Widget;

use strict;
use warnings;
use Carp qw(croak);
use List::Util 1.45 qw(sum0);
no autovivification;
require Exporter;

sub new {
    my ($class, %args) = @_;
    return bless { %args }, $class;
}

sub name {
    my $self = shift;
    return $self->{name};
}

1;
`)
	funcs, types, imports := extractPerlSymbols(src)
	sort.Strings(funcs)
	sort.Strings(types)
	sort.Strings(imports)

	if !reflect.DeepEqual(types, []string{"Acme::Widget"}) {
		t.Errorf("types = %v, want [Acme::Widget]", types)
	}
	if !reflect.DeepEqual(funcs, []string{"name", "new"}) {
		t.Errorf("functions = %v, want [name new]", funcs)
	}
	wantImports := []string{"Carp", "Exporter", "List::Util", "autovivification", "strict", "warnings"}
	if !reflect.DeepEqual(imports, wantImports) {
		t.Errorf("imports = %v, want %v", imports, wantImports)
	}
}

func TestExtractPerlSymbols_PODIgnored(t *testing.T) {
	// POD blocks contain code-shaped text that must NOT be matched.
	src := []byte(`package T;

=pod

The following subs are intentionally NOT real:

  sub poddy_fake { ... }

=cut

sub real_one { return 1; }

=head1 SYNOPSIS

  use T;
  sub another_fake { ... }

=head2 More

=cut

sub real_two { return 2; }
`)
	funcs, _, _ := extractPerlSymbols(src)
	sort.Strings(funcs)
	wantFuncs := []string{"real_one", "real_two"}
	if !reflect.DeepEqual(funcs, wantFuncs) {
		t.Errorf("functions = %v, want %v (POD bodies must not match)", funcs, wantFuncs)
	}
}

func TestExtractPerlSymbols_EndMarkerStopsScan(t *testing.T) {
	// __END__ marks the end of the source proper. Anything below
	// (typically POD or arbitrary data) must NOT be scanned.
	src := []byte(`package T;
sub real_one {}
__END__
sub fake_after_end {}
package Hidden::Pkg;
`)
	funcs, types, _ := extractPerlSymbols(src)
	if !reflect.DeepEqual(funcs, []string{"real_one"}) {
		t.Errorf("functions = %v, want [real_one]", funcs)
	}
	if !reflect.DeepEqual(types, []string{"T"}) {
		t.Errorf("types = %v, want [T] (Hidden::Pkg is past __END__)", types)
	}
}

func TestExtractPerlSymbols_DATAMarkerStopsScan(t *testing.T) {
	src := []byte(`package T;
sub real {}
__DATA__
sub fake {}
`)
	funcs, _, _ := extractPerlSymbols(src)
	if !reflect.DeepEqual(funcs, []string{"real"}) {
		t.Errorf("functions = %v, want [real]", funcs)
	}
}

func TestExtractPerlSymbols_ModernClassAndRole(t *testing.T) {
	// Perl 5.38+ experimental::class plus Moose / Moo idioms.
	src := []byte(`use experimental 'class';

class Point {
    field $x :param;
    field $y :param;
    method coords { return ($x, $y); }
}

role Loggable {
    requires 'log';
}
`)
	funcs, types, _ := extractPerlSymbols(src)
	sort.Strings(types)
	if !reflect.DeepEqual(types, []string{"Loggable", "Point"}) {
		t.Errorf("types = %v, want [Loggable Point]", types)
	}
	// Note: `method coords` doesn't match perlSubRe (uses `sub`).
	// Capturing `method` requires a separate regex; deferred. Confirm
	// no false-positives leak.
	if contains(funcs, "field") || contains(funcs, "method") || contains(funcs, "requires") {
		t.Errorf("keyword false-positive in functions: %v", funcs)
	}
}

func TestExtractPerlSymbols_BlockPackage(t *testing.T) {
	// package Foo { ... } block form (5.14+).
	src := []byte(`package Outer {
    sub inner_method {}
}
package Other;
sub other_method {}
`)
	funcs, types, _ := extractPerlSymbols(src)
	sort.Strings(funcs)
	sort.Strings(types)
	if !reflect.DeepEqual(types, []string{"Other", "Outer"}) {
		t.Errorf("types = %v, want [Other Outer]", types)
	}
	if !reflect.DeepEqual(funcs, []string{"inner_method", "other_method"}) {
		t.Errorf("functions = %v", funcs)
	}
}

func TestExtractPerlSymbols_AnonymousSubsSkipped(t *testing.T) {
	src := []byte(`my $cb = sub { return 1; };
my $other = sub ($x) { $x * 2 };
sub named_one { return 1; }
`)
	funcs, _, _ := extractPerlSymbols(src)
	// Anonymous subs (sub { ... } with no name immediately after)
	// don't match perlSubRe. Only named_one is captured.
	if !reflect.DeepEqual(funcs, []string{"named_one"}) {
		t.Errorf("functions = %v, want [named_one] only", funcs)
	}
}

func TestExtractPerlSymbols_MultiLevelPackageName(t *testing.T) {
	src := []byte(`package Foo::Bar::Baz::Quux;
sub do_it {}
`)
	_, types, _ := extractPerlSymbols(src)
	if !reflect.DeepEqual(types, []string{"Foo::Bar::Baz::Quux"}) {
		t.Errorf("types = %v, want [Foo::Bar::Baz::Quux]", types)
	}
}

func TestExtractPerlSymbols_PredeclaredSub(t *testing.T) {
	// `sub name;` is a valid predeclaration. Should still capture.
	src := []byte(`package T;
sub forward_decl;
sub implemented { return 1; }
`)
	funcs, _, _ := extractPerlSymbols(src)
	sort.Strings(funcs)
	if !reflect.DeepEqual(funcs, []string{"forward_decl", "implemented"}) {
		t.Errorf("functions = %v", funcs)
	}
}

func TestExtractPerlSymbols_Empty(t *testing.T) {
	funcs, types, imports := extractPerlSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestExtractPerlSymbols_UseVersionLine(t *testing.T) {
	// `use 5.010;` / `use v5.36;` — version lines that aren't real
	// module imports. The regex requires a letter / underscore start
	// for the captured name, so numeric versions DON'T match.
	src := []byte(`use 5.010;
use v5.36;
use strict;
use warnings;
`)
	_, _, imports := extractPerlSymbols(src)
	sort.Strings(imports)
	if !reflect.DeepEqual(imports, []string{"strict", "warnings"}) {
		t.Errorf("imports = %v, want [strict warnings] (version lines skipped)", imports)
	}
}

func TestIsPerlPODDirective(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"=pod", true},
		{"=head1 NAME", true},
		{"=cut", true},
		{"=over 4", true},
		{"==", false},
		{"=~", false},
		{"=", false},
		{"=>", false},
	} {
		if got := isPerlPODDirective(c.in); got != c.want {
			t.Errorf("isPerlPODDirective(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
