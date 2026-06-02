package content

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractPHPSymbols_SimpleClass(t *testing.T) {
	src := []byte(`<?php

namespace App\Services;

use Psr\Log\LoggerInterface;
use App\Repositories\OrderRepository as Repo;
use function App\Helpers\format_money;
use const App\Config\MAX_ORDERS;

class OrderService {
    public function __construct(LoggerInterface $log) {
        $this->log = $log;
    }

    public function processOrder(string $id): Order {
        return $this->repo->find($id);
    }

    private function log(string $msg): void {
        echo $msg;
    }
}
`)
	funcs, types, imports := extractPHPSymbols(src)
	sort.Strings(funcs)
	sort.Strings(types)
	sort.Strings(imports)

	if !contains(types, "OrderService") {
		t.Errorf("types missing OrderService: %v", types)
	}
	for _, want := range []string{"__construct", "processOrder", "log"} {
		if !contains(funcs, want) {
			t.Errorf("functions missing %q: %v", want, funcs)
		}
	}
	wantImports := []string{
		"App\\Config\\MAX_ORDERS",
		"App\\Helpers\\format_money",
		"App\\Repositories\\OrderRepository",
		"Psr\\Log\\LoggerInterface",
	}
	if !reflect.DeepEqual(imports, wantImports) {
		t.Errorf("imports = %v, want %v", imports, wantImports)
	}
}

func TestExtractPHPSymbols_InterfaceTraitEnum(t *testing.T) {
	src := []byte(`<?php

interface OrderRepositoryInterface {
    public function find(string $id): ?Order;
}

trait Loggable {
    public function logIt(string $msg): void {}
}

enum Status: string {
    case Active = 'active';
    case Inactive = 'inactive';
}

final class Locked {}
abstract class Animal {}
readonly class Frozen {}
`)
	funcs, types, _ := extractPHPSymbols(src)
	for _, want := range []string{"OrderRepositoryInterface", "Loggable", "Status", "Locked", "Animal", "Frozen"} {
		if !contains(types, want) {
			t.Errorf("types missing %s: %v", want, types)
		}
	}
	for _, want := range []string{"find", "logIt"} {
		if !contains(funcs, want) {
			t.Errorf("functions missing %s: %v", want, funcs)
		}
	}
}

func TestExtractPHPSymbols_AnonymousClosuresSkipped(t *testing.T) {
	// Anonymous closures and arrow functions have NO name — they
	// must not produce a "function" entry.
	src := []byte(`<?php

$handler = function ($req) {
    return $req->id;
};

$double = fn ($x) => $x * 2;

function realOne() { return 1; }
`)
	funcs, _, _ := extractPHPSymbols(src)
	if !contains(funcs, "realOne") {
		t.Errorf("expected realOne in functions: %v", funcs)
	}
	for _, leak := range []string{"$handler", "function", "fn", ""} {
		if contains(funcs, leak) {
			t.Errorf("anonymous closure leaked into functions as %q: %v", leak, funcs)
		}
	}
	if len(funcs) != 1 {
		t.Errorf("expected exactly 1 function (realOne), got %d: %v", len(funcs), funcs)
	}
}

func TestExtractPHPSymbols_GroupedUse(t *testing.T) {
	// `use Foo\Bar\{Baz, Qux};` — only the prefix is recorded.
	src := []byte(`<?php

use App\Models\{User, Order, Product};
`)
	_, _, imports := extractPHPSymbols(src)
	if !reflect.DeepEqual(imports, []string{"App\\Models"}) {
		t.Errorf("grouped use should record prefix only, got %v", imports)
	}
}

func TestExtractPHPSymbols_AbstractAndStaticMethods(t *testing.T) {
	src := []byte(`<?php

abstract class Base {
    abstract public function process(string $id): void;
    public static function instance(): self {
        return new static();
    }
    final protected function locked(): int { return 0; }
}
`)
	funcs, _, _ := extractPHPSymbols(src)
	for _, want := range []string{"process", "instance", "locked"} {
		if !contains(funcs, want) {
			t.Errorf("functions missing %q: %v", want, funcs)
		}
	}
}

func TestExtractPHPSymbols_ReferenceReturn(t *testing.T) {
	// PHP supports `function &name()` for reference-return.
	src := []byte(`<?php

function &getRef(): array {
    static $arr = [];
    return $arr;
}
`)
	funcs, _, _ := extractPHPSymbols(src)
	if !contains(funcs, "getRef") {
		t.Errorf("expected getRef in functions: %v", funcs)
	}
}

func TestExtractPHPSymbols_UseAliased(t *testing.T) {
	src := []byte(`<?php

use App\Long\Namespace\Path\FooBar as Foo;
use App\Other as O;
`)
	_, _, imports := extractPHPSymbols(src)
	sort.Strings(imports)
	want := []string{"App\\Long\\Namespace\\Path\\FooBar", "App\\Other"}
	if !reflect.DeepEqual(imports, want) {
		t.Errorf("imports = %v, want %v", imports, want)
	}
}

func TestExtractPHPSymbols_Empty(t *testing.T) {
	funcs, types, imports := extractPHPSymbols([]byte{})
	if len(funcs) != 0 || len(types) != 0 || len(imports) != 0 {
		t.Errorf("empty input should yield empty results, got %v/%v/%v", funcs, types, imports)
	}
}

func TestExtractPHPSymbols_NoNamespace(t *testing.T) {
	// Classic PHP without namespace — just a top-level function.
	src := []byte(`<?php

function helper($x) { return $x; }
class Widget {}
`)
	funcs, types, _ := extractPHPSymbols(src)
	if !contains(funcs, "helper") {
		t.Errorf("expected helper: %v", funcs)
	}
	if !contains(types, "Widget") {
		t.Errorf("expected Widget: %v", types)
	}
}
