package content

import (
	"sort"
	"testing"
)

func TestExtractScalaSymbols_SimpleObject(t *testing.T) {
	src := []byte(`package com.example.service

import scala.collection.mutable.ListBuffer
import cats.effect.IO

object OrderService {
  def process(id: String): IO[Order] = repo.find(id)

  private def log(msg: String): Unit = println(msg)
}
`)
	funcs, types, imports := extractScalaSymbols(src)
	sort.Strings(funcs)
	sort.Strings(types)
	sort.Strings(imports)

	if !contains(types, "OrderService") {
		t.Errorf("types missing OrderService: %v", types)
	}
	for _, want := range []string{"process", "log"} {
		if !contains(funcs, want) {
			t.Errorf("functions missing %q: %v", want, funcs)
		}
	}
	for _, want := range []string{"scala.collection.mutable.ListBuffer", "cats.effect.IO"} {
		if !contains(imports, want) {
			t.Errorf("imports missing %q: %v", want, imports)
		}
	}
}

func TestExtractScalaSymbols_CaseClassTraitEnum(t *testing.T) {
	src := []byte(`sealed trait Shape
case class Circle(r: Double) extends Shape
case class Square(side: Double) extends Shape
abstract class Base
final class Derived extends Base
case object Empty
enum Color { case Red, Green, Blue }
`)
	_, types, _ := extractScalaSymbols(src)
	sort.Strings(types)

	for _, want := range []string{"Shape", "Circle", "Square", "Base", "Derived", "Empty", "Color"} {
		if !contains(types, want) {
			t.Errorf("types missing %q: %v", want, types)
		}
	}
}

func TestExtractScalaSymbols_Imports(t *testing.T) {
	src := []byte(`import scala.collection._
import java.util.*
import scala.collection.{List, Map => ImmutableMap}
import a.b.c.Foo
`)
	_, _, imports := extractScalaSymbols(src)
	sort.Strings(imports)

	for _, want := range []string{
		"scala.collection._",  // Scala 2 wildcard
		"java.util.*",         // Scala 3 wildcard
		"scala.collection",    // grouped selector → prefix only
		"a.b.c.Foo",           // plain
	} {
		if !contains(imports, want) {
			t.Errorf("imports missing %q: %v", want, imports)
		}
	}
}

func TestExtractScalaSymbols_DefVariants(t *testing.T) {
	src := []byte(`class Calc {
  def add(a: Int, b: Int): Int = a + b
  def +(other: Calc): Calc = combine(other)
  override def toString: String = "Calc"
  def generic[T](x: T): T = x
  private def secret = 42
  def ` + "`match`" + `(): Unit = ()
}
`)
	funcs, _, _ := extractScalaSymbols(src)
	sort.Strings(funcs)

	for _, want := range []string{"add", "+", "toString", "generic", "secret", "match"} {
		if !contains(funcs, want) {
			t.Errorf("functions missing %q: %v", want, funcs)
		}
	}
}

func TestExtractScalaSymbols_Empty(t *testing.T) {
	funcs, types, imports := extractScalaSymbols([]byte(""))
	if funcs != nil || types != nil || imports != nil {
		t.Errorf("expected all-nil for empty input, got funcs=%v types=%v imports=%v", funcs, types, imports)
	}
}
