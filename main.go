// This file is part of go-antlr-calc.
//
// Copyright (C) 2020  David Gamba Rios
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:generate ./antlr4 -Xexact-output-dir -o parser -visitor -Dlanguage=Go ./LabeledExpr.g4
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/DavidGamba/go-antlr-calc/parser"

	"github.com/DavidGamba/go-getoptions"
	"github.com/antlr/antlr4/runtime/Go/antlr"
)

var logger = log.New(ioutil.Discard, "", log.LstdFlags)

// Echo input
var echo bool

func main() {
	var file string
	opt := getoptions.New()
	opt.Self("", "Antlr based calculator.\n\n    Call with no arguments to enter repl")
	opt.HelpSynopsisArgs("[<single statement>]")
	opt.Bool("help", false, opt.Alias("?"))
	opt.Bool("debug", false, opt.Description("Show debug output"))
	opt.BoolVar(&echo, "echo", false, opt.Description("Enable echo in REPL"))
	opt.StringVar(&file, "file", "",
		opt.Description("Read statements from file"), opt.ArgName("filename"))
	remaining, err := opt.Parse(os.Args[1:])
	if opt.Called("help") {
		fmt.Fprintf(os.Stderr, opt.Help())
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
	if opt.Called("debug") {
		logger.SetOutput(os.Stderr)
	}
	logger.Println(remaining)

	visitor := NewCalcVisitor()

	if opt.Called("file") {
		echo = true
		contents, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to read file '%s': %s\n", remaining[0], err)
			os.Exit(1)
		}
		err = parseInput(visitor, string(contents))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
			os.Exit(1)
		}
	} else if len(remaining) > 0 {
		err = parseInput(visitor, strings.Join(remaining, " ")+"\n")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
			os.Exit(1)
		}
	} else {
		for {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("> ")
			text, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					fmt.Println("")
					os.Exit(0)
				}
				fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
				os.Exit(1)
			}
			err = parseInput(visitor, text)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
			}
		}
	}
}

func parseInput(visitor *CalcVisitor, input string) error {
	inputStream := antlr.NewInputStream(input)
	lexer := parser.NewLabeledExprLexer(inputStream)
	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := parser.NewLabeledExprParser(tokens)
	p.RemoveErrorListeners()
	p.AddErrorListener(new(CalcErrorListener))
	p.AddErrorListener(antlr.NewDiagnosticErrorListener(false))
	// TODO: BailErrorStrategy is broken right now
	// p.SetErrorHandler(antlr.NewBailErrorStrategy())
	tree := p.Prog()
	visitor.Visit(tree)
	return nil
}

// CalcErrorListener - Implements the antlr.ErrorListener interface
type CalcErrorListener struct {
	*antlr.DefaultErrorListener
}

func (ce *CalcErrorListener) SyntaxError(
	recognizer antlr.Recognizer,
	offendingSymbol interface{},
	line, column int,
	msg string,
	e antlr.RecognitionException) {
	fmt.Fprintf(os.Stderr, "ERROR: line %d:%d %s\n", line, column, msg)
}

// underlineError - Couldn't get this to work
// Book Chapter 9, page 158
func underlineError(
	recognizer antlr.Recognizer,
	offendingSymbol interface{},
	line, column int) {
}

// func (ce *CalcErrorListener) ReportAmbiguity(
// 	recognizer antlr.Parser,
// 	dfa *antlr.DFA,
// 	startIndex, stopIndex int,
// 	exact bool,
// 	ambigAlts *antlr.BitSet,
// 	configs antlr.ATNConfigSet) {
// }
// func (ce *CalcErrorListener) ReportAttemptingFullContext(
// 	recognizer antlr.Parser,
// 	dfa *antlr.DFA,
// 	startIndex, stopIndex int,
// 	conflictingAlts *antlr.BitSet,
// 	configs antlr.ATNConfigSet) {
// }
// func (ce *CalcErrorListener) ReportContextSensitivity(
// 	recognizer antlr.Parser,
// 	dfa *antlr.DFA,
// 	startIndex, stopIndex, prediction int,
// 	configs antlr.ATNConfigSet) {
// }

var ErrorBlank = errors.New("Blank Line")

// CalcReturn - Visitor parent only returns interface{}
// Need something we can actually work with.
type CalcReturn struct {
	Value int
	Error error
}

type CalcVisitor struct {
	parser.BaseLabeledExprVisitor

	memory map[string]int
}

func NewCalcVisitor() *CalcVisitor {
	m := make(map[string]int)
	return &CalcVisitor{memory: m}
}

// Visit - Returns a CalcReturn.
func (c *CalcVisitor) Visit(tree antlr.ParseTree) interface{} {
	logger.Printf("visit input type: %v\n", reflect.TypeOf(tree))

	switch t := tree.(type) {
	case *antlr.ErrorNodeImpl:
		return CalcReturn{0, fmt.Errorf("syntax error near '%s'", t.GetText())}
	default:
		if cr, ok := tree.Accept(c).(CalcReturn); ok {
			return cr
		}
	}

	return CalcReturn{0, fmt.Errorf("visit result not of type CalcReturn")}
}

// VisitChildren - This visit children implementation is only used for the prog node so it prints the statement.
func (c *CalcVisitor) VisitChildren(node antlr.RuleNode) interface{} {
	for _, n := range node.GetChildren() {
		logger.Printf("child: %s", n)
		if echo {
			fmt.Printf("> %s", n.(antlr.ParseTree).GetText())
		}
		cr := c.Visit(n.(antlr.ParseTree)).(CalcReturn)
		if cr.Error != nil {
			if errors.Is(cr.Error, ErrorBlank) {
				continue
			}
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", cr.Error)
			continue
		}
		fmt.Printf("  %d\n", cr.Value)
	}
	return CalcReturn{0, nil}
}

func (c *CalcVisitor) VisitProg(ctx *parser.ProgContext) interface{} {
	logger.Printf("Calculating Programm: %s", ctx.GetText())
	return c.VisitChildren(ctx)
}

func (c *CalcVisitor) VisitPrintExpr(ctx *parser.PrintExprContext) interface{} {
	logger.Printf("VisitPrintExpr: %s", ctx.GetText())
	return c.Visit(ctx.Expr()).(CalcReturn)
}

func (c *CalcVisitor) VisitAssign(ctx *parser.AssignContext) interface{} {
	logger.Printf("VisitAssign: %s", ctx.GetText())
	id := ctx.ID().GetText()
	cr := c.Visit(ctx.Expr()).(CalcReturn)
	if cr.Error != nil {
		return CalcReturn{0, fmt.Errorf("error with assignment '%s': %w", ctx.GetText(), cr.Error)}
	}
	c.memory[id] = cr.Value
	return cr
}

func (c *CalcVisitor) VisitBlank(ctx *parser.BlankContext) interface{} {
	return CalcReturn{0, ErrorBlank}
}

func (c *CalcVisitor) VisitParens(ctx *parser.ParensContext) interface{} {
	logger.Printf("VisitParens: %s", ctx.GetText())
	return c.Visit(ctx.Expr()).(CalcReturn)
}

func (c *CalcVisitor) VisitMulDiv(ctx *parser.MulDivContext) interface{} {
	logger.Printf("VisitMulDiv: %s\n", ctx.GetText())
	crLeft := c.Visit(ctx.Expr(0)).(CalcReturn)
	if crLeft.Error != nil {
		return CalcReturn{0, fmt.Errorf("error with left side visit '%s': %w", ctx.Expr(0).GetText(), crLeft.Error)}
	}
	crRight := c.Visit(ctx.Expr(1)).(CalcReturn)
	if crRight.Error != nil {
		return CalcReturn{0, fmt.Errorf("error with right side visit '%s': %w", ctx.Expr(1).GetText(), crRight.Error)}
	}
	operator := ctx.GetOp().GetTokenType()
	if operator == parser.LabeledExprLexerMUL {
		return CalcReturn{crLeft.Value * crRight.Value, nil}
	}
	if operator == parser.LabeledExprLexerDIV {
		return CalcReturn{crLeft.Value / crRight.Value, nil}
	}
	return CalcReturn{0, fmt.Errorf("wrong operator '%v'", operator)}
}

func (c *CalcVisitor) VisitAddSub(ctx *parser.AddSubContext) interface{} {
	logger.Printf("VisitAddSub: %s\n", ctx.GetText())
	crLeft := c.Visit(ctx.Expr(0)).(CalcReturn)
	if crLeft.Error != nil {
		return CalcReturn{0, fmt.Errorf("error with left side visit '%s': %w", ctx.Expr(0).GetText(), crLeft.Error)}
	}
	crRight := c.Visit(ctx.Expr(1)).(CalcReturn)
	if crRight.Error != nil {
		return CalcReturn{0, fmt.Errorf("error with right side visit '%s': %w", ctx.Expr(1).GetText(), crRight.Error)}
	}
	operator := ctx.GetOp().GetTokenType()
	if operator == parser.LabeledExprLexerADD {
		return CalcReturn{crLeft.Value + crRight.Value, nil}
	}
	if operator == parser.LabeledExprLexerSUB {
		return CalcReturn{crLeft.Value - crRight.Value, nil}
	}
	return CalcReturn{0, fmt.Errorf("wrong operator '%v'", operator)}
}

func (c *CalcVisitor) VisitId(ctx *parser.IdContext) interface{} {
	logger.Printf("VisitID: %s", ctx.GetText())
	id := ctx.ID().GetText()
	if value, ok := c.memory[id]; ok {
		return CalcReturn{value, nil}
	}
	return CalcReturn{0, fmt.Errorf("undefined ID '%s'", id)}
}

func (c *CalcVisitor) VisitInt(ctx *parser.IntContext) interface{} {
	logger.Printf("VisitInt: %s\n", ctx.GetText())
	i, err := strconv.Atoi(ctx.GetText())
	if err != nil {
		return CalcReturn{0, fmt.Errorf("couldn't parse integer: '%s': %w", ctx.GetText(), err)}
	}
	return CalcReturn{i, nil}
}
