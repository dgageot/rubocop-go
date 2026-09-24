package cops

import (
	"go/ast"
	"go/types"
	"slices"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintConstructorNetworkIO enforces that constructors do not perform network I/O.
//
// Constructors should assemble state and return it. Dialing, listening,
// issuing HTTP requests, or resolving DNS names from New* hides network side
// effects before the caller can decide when to connect, arrange cancellation,
// or surface failures from an explicit operation.
//
// Detection is intentionally low-noise: only calls to selected functions and
// methods in net and net/http are flagged (net.Dial*/Listen*/Lookup*,
// Resolver.Lookup*, http.Get/Head/Post/PostForm). Method calls such as .Do
// and .Accept are out of scope for now.
//
// Calls inside nested function literals are ignored unless the literal is
// immediately invoked as part of constructor execution.
//
// Annotate an intentional case with //rubocop:disable Lint/ConstructorNetworkIO.
func NewLintConstructorNetworkIO(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/ConstructorNetworkIO",
			Description: "Avoid network I/O in constructors.",
			Severity:    cop.Error,
		},
		Types: true,
		Run: func(p *cop.Pass) {
			p.ForEachFunc(func(fn *ast.FuncDecl) {
				if !isConstructor(fn) || fn.Body == nil {
					return
				}
				forEachConstructionCallExpr(fn.Body, func(call *ast.CallExpr) {
					pkg, name, ok := networkIOCall(p, call)
					if !ok {
						return
					}
					p.Reportf(call,
						"constructor %s calls %s.%s; move network I/O out of New into Start/Connect or the first request path",
						fn.Name.Name, pkg, name)
				})
			})
		},
	}, opts...)
}

func networkIOCall(p *cop.Pass, call *ast.CallExpr) (string, string, bool) {
	if pkg, name, ok := networkIOFuncName(calleeObject(p.Info, call)); ok {
		return pkg, name, true
	}

	if name, ok := cop.CallTo(call, "net", netIOFuncNames...); ok {
		return "net", name, true
	}
	if name, ok := cop.CallTo(call, "http", "Get", "Head", "Post", "PostForm"); ok {
		return "http", name, true
	}
	return "", "", false
}

// netIOFuncNames are the functions (and Resolver methods, matched by the
// type-based path) in package net that perform network I/O: dialing,
// listening, and DNS resolution.
var netIOFuncNames = []string{
	"Dial", "DialTimeout", "Listen", "ListenPacket", "ListenTCP", "ListenUDP", "ListenUnix",
	"LookupAddr", "LookupCNAME", "LookupHost", "LookupIP", "LookupIPAddr",
	"LookupMX", "LookupNS", "LookupNetIP", "LookupPort", "LookupSRV", "LookupTXT",
}

func networkIOFuncName(obj types.Object) (string, string, bool) {
	fn, ok := obj.(*types.Func)
	if !ok {
		return "", "", false
	}
	pkg := fn.Pkg()
	if pkg == nil {
		return "", "", false
	}
	name := fn.Name()
	switch pkg.Path() {
	case "net":
		// Matches both package functions and *net.Resolver methods
		// (e.g. net.DefaultResolver.LookupIPAddr).
		if slices.Contains(netIOFuncNames, name) {
			return "net", name, true
		}
	case "net/http":
		switch name {
		case "Get", "Head", "Post", "PostForm":
			return "http", name, true
		}
	}
	return "", "", false
}
