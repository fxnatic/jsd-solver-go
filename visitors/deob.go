package visitors

import (
	"fmt"
	"math"
	"strings"

	"github.com/t14raptor/go-fast/ast"
)

type deobVisitor struct {
	ast.NoopVisitor
	numbers map[ast.Id]map[string]float64
	strings map[float64]string
	aliases map[string]struct{}
}

func (v *deobVisitor) VisitStatement(n *ast.Statement) {
	n.VisitChildrenWith(v)
}

func (v *deobVisitor) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	switch expr := n.Expr.(type) {
	case *ast.AssignExpression:
		if expr.Operator.String() != "=" {
			return
		}

		if left, ok := expr.Left.Expr.(*ast.Identifier); ok {
			rightExpr := unwrapSequenceTail(expr.Right.Expr)
			if right, ok := rightExpr.(*ast.Identifier); ok && v.isAlias(right.Name) {
				v.aliases[left.Name] = struct{}{}
			}
		}

		obj, ok := expr.Right.Expr.(*ast.ObjectLiteral)
		if !ok {
			return
		}
		left, ok := expr.Left.Expr.(*ast.Identifier)
		if !ok {
			return
		}

		if m, ok := captureNumericObjectMap(obj); ok {
			v.numbers[left.ToId()] = m
		}
	case *ast.MemberExpression:
		id, ok := expr.Object.Expr.(*ast.Identifier)
		if !ok {
			return
		}

		propName, ok := memberPropName(expr.Property)
		if !ok {
			return
		}

		propMap := v.numbers[id.ToId()]
		if propMap == nil {
			return
		}

		val := propMap[propName]
		n.Expr = &ast.NumberLiteral{
			Value: val,
		}
	case *ast.CallExpression:
		if callee, ok := expr.Callee.Expr.(*ast.Identifier); ok && v.isAlias(callee.Name) && len(expr.ArgumentList) == 1 {
			arg, ok := expr.ArgumentList[0].Expr.(*ast.NumberLiteral)
			if !ok || v.strings == nil {
				return
			}
			if val, ok := v.strings[arg.Value]; ok {
				n.Expr = &ast.StringLiteral{Value: val}
				return
			}
		}

		if callee, ok := expr.Callee.Expr.(*ast.Identifier); ok && callee.Name == "parseInt" && len(expr.ArgumentList) >= 1 {
			if str, ok := expr.ArgumentList[0].Expr.(*ast.StringLiteral); ok {
				n.Expr = &ast.NumberLiteral{Value: jsParseInt(str.Value)}
			}
			return
		}

		if member, ok := expr.Callee.Expr.(*ast.MemberExpression); ok && len(expr.ArgumentList) == 1 {
			if obj, ok := member.Object.Expr.(*ast.Identifier); ok && obj.Name == "Math" {
				if prop, ok := member.Property.Prop.(*ast.Identifier); ok && prop.Name == "floor" {
					if arg, ok := expr.ArgumentList[0].Expr.(*ast.NumberLiteral); ok {
						n.Expr = &ast.NumberLiteral{Value: math.Floor(arg.Value)}
					}
					return
				}
			}
		}
	case *ast.UnaryExpression:
		switch expr.Operator.String() {
		case "!":
			switch val := expr.Operand.Expr.(type) {
			case *ast.BooleanLiteral:
				n.Expr = &ast.BooleanLiteral{Value: !val.Value}
			case *ast.ArrayLiteral, *ast.ObjectLiteral:
				n.Expr = &ast.BooleanLiteral{Value: false}
			}
		case "-":
			if num, ok := expr.Operand.Expr.(*ast.NumberLiteral); ok {
				n.Expr = &ast.NumberLiteral{Value: -num.Value}
			}
		case "+":
			if num, ok := expr.Operand.Expr.(*ast.NumberLiteral); ok {
				n.Expr = &ast.NumberLiteral{Value: num.Value}
			}
		}
	case *ast.BinaryExpression:
		left, lok := expr.Left.Expr.(*ast.NumberLiteral)
		right, rok := expr.Right.Expr.(*ast.NumberLiteral)
		if !lok || !rok {
			return
		}

		switch expr.Operator.String() {
		case "+":
			n.Expr = &ast.NumberLiteral{Value: left.Value + right.Value}
		case "-":
			n.Expr = &ast.NumberLiteral{Value: left.Value - right.Value}
		case "*":
			n.Expr = &ast.NumberLiteral{Value: left.Value * right.Value}
		case "/":
			if right.Value != 0 {
				n.Expr = &ast.NumberLiteral{Value: left.Value / right.Value}
			}
		case "%":
			if right.Value != 0 {
				n.Expr = &ast.NumberLiteral{Value: math.Mod(left.Value, right.Value)}
			}
		}
	}
}

func captureNumericObjectMap(obj *ast.ObjectLiteral) (map[string]float64, bool) {
	if obj == nil {
		return nil, false
	}

	out := make(map[string]float64)
	for _, entry := range obj.Value {
		prop, ok := entry.Prop.(*ast.PropertyKeyed)
		if !ok {
			return nil, false
		}
		keyName, ok := literalKeyName(prop.Key)
		if !ok {
			return nil, false
		}
		if prop.Value == nil || prop.Value.Expr == nil {
			return nil, false
		}

		val, ok := evalNumericLiteral(prop.Value.Expr)
		if !ok {
			return nil, false
		}
		out[keyName] = val
	}

	if len(out) < 2 {
		return nil, false
	}
	return out, true
}

func evalNumericLiteral(e ast.Expr) (float64, bool) {
	switch v := e.(type) {
	case *ast.NumberLiteral:
		return v.Value, true
	case *ast.UnaryExpression:
		if v.Operand == nil || v.Operand.Expr == nil {
			return 0, false
		}
		num, ok := v.Operand.Expr.(*ast.NumberLiteral)
		if !ok {
			return 0, false
		}
		switch v.Operator.String() {
		case "-":
			return -num.Value, true
		case "+":
			return num.Value, true
		default:
			return 0, false
		}
	default:
		return 0, false
	}
}

type DeobfuscateResult struct {
	LZAlphabet string
}

func DeobfuscateCf(p *ast.Program) (*DeobfuscateResult, error) {
	inlineConstantObjects(p)

	offset := extractOffset(p)
	if offset == 0 {
		return nil, fmt.Errorf("failed at step 1: could not extract decoder offset")
	}

	target := extractTarget(p)
	if target == 0 {
		return nil, fmt.Errorf("failed at step 2: could not extract rotation target")
	}

	rotationExpr := extractRotationExpr(p)
	if rotationExpr.Expr == nil {
		return nil, fmt.Errorf("failed at step 3: could not extract rotation expression")
	}

	aliases, err := collectAliases(p)
	if err != nil {
		return nil, fmt.Errorf("failed at step 4: %w", err)
	}

	strings := buildstringsDynamic(p, offset, target, rotationExpr, aliases)
	if strings == nil {
		return nil, fmt.Errorf("failed at step 5: could not build string map (no string table found)")
	}

	f := &deobVisitor{
		numbers: make(map[ast.Id]map[string]float64),
		strings: strings,
		aliases: aliases,
	}

	f.V = f
	p.VisitWith(f)

	alphabet := extractLZAlphabet(p)
	if alphabet == "" {
		return nil, fmt.Errorf("failed at step 6: could not extract LZ alphabet (no 64-char string in charAt call found)")
	}

	return &DeobfuscateResult{
		LZAlphabet: alphabet,
	}, nil
}

func extractLZAlphabet(p *ast.Program) string {
	var alphabet string
	findAlphabetInNode(p, &alphabet)
	return alphabet
}

func findAlphabetInNode(node ast.Node, alphabet *string) {
	if node == nil || *alphabet != "" {
		return
	}

	if expr, ok := node.(*ast.Expression); ok && expr != nil && expr.Expr != nil {
		if call, ok := expr.Expr.(*ast.CallExpression); ok {
			if found := checkCallForAlphabet(call); found != "" {
				*alphabet = found
				return
			}
		}
	}

	if call, ok := node.(*ast.CallExpression); ok && call != nil {
		if found := checkCallForAlphabet(call); found != "" {
			*alphabet = found
			return
		}
	}

	switch n := node.(type) {
	case *ast.Program:
		if n == nil {
			return
		}
		for i := range n.Body {
			findAlphabetInNode(&n.Body[i], alphabet)
		}
	case *ast.Statement:
		findAlphabetInStatement(n, alphabet)
	case *ast.Expression:
		findAlphabetInExpression(n, alphabet)
	case *ast.BlockStatement:
		if n == nil {
			return
		}
		for i := range n.List {
			findAlphabetInNode(&n.List[i], alphabet)
		}
	}
}

func findAlphabetInStatement(stmt *ast.Statement, alphabet *string) {
	if stmt == nil || stmt.Stmt == nil || *alphabet != "" {
		return
	}

	switch s := stmt.Stmt.(type) {
	case *ast.ExpressionStatement:
		if s != nil && s.Expression != nil {
			findAlphabetInExpression(s.Expression, alphabet)
		}
	case *ast.ReturnStatement:
		if s != nil && s.Argument != nil {
			findAlphabetInExpression(s.Argument, alphabet)
		}
	case *ast.IfStatement:
		if s == nil {
			return
		}
		if s.Test != nil {
			findAlphabetInExpression(s.Test, alphabet)
		}
		if s.Consequent != nil {
			findAlphabetInStatement(s.Consequent, alphabet)
		}
		if s.Alternate != nil {
			findAlphabetInStatement(s.Alternate, alphabet)
		}
	case *ast.ForStatement:
		if s == nil {
			return
		}
		if s.Test != nil {
			findAlphabetInExpression(s.Test, alphabet)
		}
		if s.Update != nil {
			findAlphabetInExpression(s.Update, alphabet)
		}
		if s.Body != nil {
			findAlphabetInStatement(s.Body, alphabet)
		}
	case *ast.ForInStatement:
		if s == nil {
			return
		}
		if s.Source != nil {
			findAlphabetInExpression(s.Source, alphabet)
		}
		if s.Body != nil {
			findAlphabetInStatement(s.Body, alphabet)
		}
	case *ast.WhileStatement:
		if s == nil {
			return
		}
		if s.Test != nil {
			findAlphabetInExpression(s.Test, alphabet)
		}
		if s.Body != nil {
			findAlphabetInStatement(s.Body, alphabet)
		}
	case *ast.DoWhileStatement:
		if s == nil {
			return
		}
		if s.Test != nil {
			findAlphabetInExpression(s.Test, alphabet)
		}
		if s.Body != nil {
			findAlphabetInStatement(s.Body, alphabet)
		}
	case *ast.TryStatement:
		if s == nil {
			return
		}
		if s.Body != nil {
			findAlphabetInBlockStatement(s.Body, alphabet)
		}
		if s.Catch != nil && s.Catch.Body != nil {
			findAlphabetInBlockStatement(s.Catch.Body, alphabet)
		}
		if s.Finally != nil {
			findAlphabetInBlockStatement(s.Finally, alphabet)
		}
	case *ast.SwitchStatement:
		if s == nil {
			return
		}
		if s.Discriminant != nil {
			findAlphabetInExpression(s.Discriminant, alphabet)
		}
		for i := range s.Body {
			for j := range s.Body[i].Consequent {
				findAlphabetInStatement(&s.Body[i].Consequent[j], alphabet)
			}
		}
	case *ast.BlockStatement:
		findAlphabetInBlockStatement(s, alphabet)
	case *ast.FunctionDeclaration:
		if s != nil && s.Function != nil && s.Function.Body != nil {
			findAlphabetInBlockStatement(s.Function.Body, alphabet)
		}
	case *ast.VariableDeclaration:
		if s == nil {
			return
		}
		for i := range s.List {
			if s.List[i].Initializer != nil {
				findAlphabetInExpression(s.List[i].Initializer, alphabet)
			}
		}
	}
}

func findAlphabetInBlockStatement(block *ast.BlockStatement, alphabet *string) {
	if block == nil || *alphabet != "" {
		return
	}
	for i := range block.List {
		findAlphabetInStatement(&block.List[i], alphabet)
	}
}

func findAlphabetInExpression(expr *ast.Expression, alphabet *string) {
	if expr == nil || expr.Expr == nil || *alphabet != "" {
		return
	}

	if call, ok := expr.Expr.(*ast.CallExpression); ok {
		if found := checkCallForAlphabet(call); found != "" {
			*alphabet = found
			return
		}
	}

	switch e := expr.Expr.(type) {
	case *ast.CallExpression:
		if e == nil {
			return
		}
		if e.Callee != nil {
			findAlphabetInExpression(e.Callee, alphabet)
		}
		for i := range e.ArgumentList {
			findAlphabetInExpression(&e.ArgumentList[i], alphabet)
		}
	case *ast.MemberExpression:
		if e != nil && e.Object != nil {
			findAlphabetInExpression(e.Object, alphabet)
		}
	case *ast.BinaryExpression:
		if e == nil {
			return
		}
		if e.Left != nil {
			findAlphabetInExpression(e.Left, alphabet)
		}
		if e.Right != nil {
			findAlphabetInExpression(e.Right, alphabet)
		}
	case *ast.UnaryExpression:
		if e != nil && e.Operand != nil {
			findAlphabetInExpression(e.Operand, alphabet)
		}
	case *ast.AssignExpression:
		if e == nil {
			return
		}
		if e.Left != nil {
			findAlphabetInExpression(e.Left, alphabet)
		}
		if e.Right != nil {
			findAlphabetInExpression(e.Right, alphabet)
		}
	case *ast.ConditionalExpression:
		if e == nil {
			return
		}
		if e.Test != nil {
			findAlphabetInExpression(e.Test, alphabet)
		}
		if e.Consequent != nil {
			findAlphabetInExpression(e.Consequent, alphabet)
		}
		if e.Alternate != nil {
			findAlphabetInExpression(e.Alternate, alphabet)
		}
	case *ast.SequenceExpression:
		if e == nil {
			return
		}
		for i := range e.Sequence {
			findAlphabetInExpression(&e.Sequence[i], alphabet)
		}
	case *ast.ArrayLiteral:
		if e == nil {
			return
		}
		for i := range e.Value {
			findAlphabetInExpression(&e.Value[i], alphabet)
		}
	case *ast.ObjectLiteral:
		if e == nil {
			return
		}
		for i := range e.Value {
			if prop, ok := e.Value[i].Prop.(*ast.PropertyKeyed); ok && prop != nil && prop.Value != nil {
				findAlphabetInExpression(prop.Value, alphabet)
			}
		}
	case *ast.FunctionLiteral:
		if e != nil && e.Body != nil {
			findAlphabetInBlockStatement(e.Body, alphabet)
		}
	case *ast.ArrowFunctionLiteral:
	}
}

func checkCallForAlphabet(call *ast.CallExpression) string {
	if call == nil || call.Callee == nil || call.Callee.Expr == nil {
		return ""
	}

	member, ok := call.Callee.Expr.(*ast.MemberExpression)
	if !ok || member.Object == nil || member.Object.Expr == nil {
		return ""
	}

	strLit, ok := member.Object.Expr.(*ast.StringLiteral)
	if !ok {
		return ""
	}

	propName, ok := memberPropName(member.Property)
	if !ok || propName != "charAt" {
		return ""
	}

	seen := make(map[rune]bool)
	hasLetter := false
	hasDigit := false

	for _, c := range strLit.Value {
		if seen[c] {
			return ""
		}
		seen[c] = true
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
		}
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
	}

	if hasLetter && hasDigit {
		return strLit.Value
	}
	return ""
}

func inlineConstantObjects(p *ast.Program) {
	collector := &constObjCollector{
		objLits: make(map[ast.Id]map[string]*ast.Expression),
	}
	collector.V = collector
	p.VisitWith(collector)

	inliner := &constObjInliner{
		objLits: collector.objLits,
	}
	inliner.V = inliner
	p.VisitWith(inliner)
}

type constObjCollector struct {
	ast.NoopVisitor
	objLits map[ast.Id]map[string]*ast.Expression
}

func (v *constObjCollector) VisitStatement(n *ast.Statement) {
	n.VisitChildrenWith(v)

	decl, ok := n.Stmt.(*ast.VariableDeclaration)
	if !ok {
		return
	}
	for i := range decl.List {
		d := decl.List[i]
		if d.Initializer == nil || d.Target == nil || d.Target.Target == nil {
			continue
		}
		id, ok := d.Target.Target.(*ast.Identifier)
		if !ok {
			continue
		}
		obj, ok := d.Initializer.Expr.(*ast.ObjectLiteral)
		if !ok {
			continue
		}
		v.captureObjectLiteral(id, obj)
	}
}

func (v *constObjCollector) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)

	assign, ok := n.Expr.(*ast.AssignExpression)
	if !ok || assign.Operator.String() != "=" {
		return
	}
	left, ok := assign.Left.Expr.(*ast.Identifier)
	if !ok {
		return
	}
	obj, ok := assign.Right.Expr.(*ast.ObjectLiteral)
	if !ok {
		return
	}
	v.captureObjectLiteral(left, obj)
}

func (v *constObjCollector) captureObjectLiteral(left *ast.Identifier, obj *ast.ObjectLiteral) {
	id := left.ToId()
	tmp := make(map[string]*ast.Expression)
	nonNumericSeen := false
	numericCount := 0

	for _, entry := range obj.Value {
		prop, ok := entry.Prop.(*ast.PropertyKeyed)
		if !ok {
			continue
		}

		keyName, ok := literalKeyName(prop.Key)
		if !ok {
			continue
		}

		if prop.Value == nil || prop.Value.Expr == nil {
			continue
		}
		if !isInlineableNumber(prop.Value.Expr) {
			nonNumericSeen = true
			break
		}

		tmp[keyName] = prop.Value.Clone()
		numericCount++
	}

	if nonNumericSeen || numericCount < 2 {
		return
	}
	v.objLits[id] = tmp
}

type constObjInliner struct {
	ast.NoopVisitor
	objLits map[ast.Id]map[string]*ast.Expression
}

func (v *constObjInliner) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)

	mem, ok := n.Expr.(*ast.MemberExpression)
	if !ok {
		return
	}
	objId, ok := mem.Object.Expr.(*ast.Identifier)
	if !ok {
		return
	}
	propName, ok := memberPropName(mem.Property)
	if !ok {
		return
	}

	props := v.objLits[objId.ToId()]
	if props == nil {
		return
	}
	valExpr := props[propName]
	if valExpr == nil || valExpr.Expr == nil {
		return
	}

	n.Expr = valExpr.Clone().Expr
}

func literalKeyName(keyExpr *ast.Expression) (string, bool) {
	if keyExpr == nil || keyExpr.Expr == nil {
		return "", false
	}
	switch k := keyExpr.Expr.(type) {
	case *ast.Identifier:
		return k.Name, true
	case *ast.StringLiteral:
		return k.Value, true
	default:
		return "", false
	}
}

func isInlineableNumber(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.NumberLiteral:
		return true
	case *ast.UnaryExpression:
		if v.Operand == nil || v.Operand.Expr == nil {
			return false
		}
		_, ok := v.Operand.Expr.(*ast.NumberLiteral)
		return ok && (v.Operator.String() == "-" || v.Operator.String() == "+")
	default:
		return false
	}
}

func extractOffset(p *ast.Program) int {
	df := &decoderOffsetFinder{}
	df.V = df
	p.VisitWith(df)
	if df.offset != 0 {
		return df.offset
	}

	finder := &offsetFinder{}
	finder.V = finder
	p.VisitWith(finder)
	if finder.offset == 0 {
		return 406
	}
	return finder.offset
}

type offsetFinder struct {
	ast.NoopVisitor
	offset int
}

func (v *offsetFinder) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	if v.offset != 0 {
		return
	}

	assign, ok := n.Expr.(*ast.AssignExpression)
	if !ok || assign.Operator.String() != "=" {
		return
	}

	leftId, ok := assign.Left.Expr.(*ast.Identifier)
	if !ok {
		return
	}

	binary, ok := assign.Right.Expr.(*ast.BinaryExpression)
	if !ok || binary.Operator.String() != "-" {
		return
	}

	rightId, ok := binary.Left.Expr.(*ast.Identifier)
	if !ok || rightId.Name != leftId.Name {
		return
	}

	numLit, ok := binary.Right.Expr.(*ast.NumberLiteral)
	if !ok {
		return
	}

	if numLit.Value > 50 && numLit.Value < 2000 {
		v.offset = int(numLit.Value)
	}
}

type decoderOffsetFinder struct {
	ast.NoopVisitor
	offset int
}

func (v *decoderOffsetFinder) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	if v.offset != 0 {
		return
	}

	assign, ok := n.Expr.(*ast.AssignExpression)
	if !ok || assign.Operator.String() != "=" {
		return
	}

	_, ok = assign.Left.Expr.(*ast.Identifier)
	if !ok {
		return
	}
	fn, ok := assign.Right.Expr.(*ast.FunctionLiteral)
	if !ok || fn.Body == nil {
		return
	}

	finder := &offsetFinder{}
	finder.V = finder
	fn.Body.VisitWith(finder)
	if finder.offset != 0 {
		v.offset = finder.offset
	}
}

func extractTarget(p *ast.Program) int {
	finder := &targetFinder{}
	finder.V = finder
	p.VisitWith(finder)
	if finder.target == 0 {
		return 159113
	}
	return finder.target
}

type targetFinder struct {
	ast.NoopVisitor
	target int
}

type rotationExprFinder struct {
	ast.NoopVisitor
	expr ast.Expression
}

func (v *rotationExprFinder) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	if v.expr.Expr != nil {
		return
	}

	assign, ok := n.Expr.(*ast.AssignExpression)
	if !ok || assign.Operator.String() != "=" {
		return
	}

	if containsParseInt(assign.Right) {
		v.expr = *assign.Right
	}
}

func containsParseInt(expr *ast.Expression) bool {
	found := false
	var walk func(e *ast.Expression)
	walk = func(e *ast.Expression) {
		if e == nil || found {
			return
		}
		switch node := e.Expr.(type) {
		case *ast.CallExpression:
			if id, ok := node.Callee.Expr.(*ast.Identifier); ok && id.Name == "parseInt" {
				found = true
				return
			}
			walk(node.Callee)
			for i := range node.ArgumentList {
				walk(&node.ArgumentList[i])
			}
		case *ast.BinaryExpression:
			walk(node.Left)
			walk(node.Right)
		case *ast.UnaryExpression:
			walk(node.Operand)
		}
	}
	walk(expr)
	return found
}

func extractRotationExpr(p *ast.Program) ast.Expression {
	f := &rotationExprFinder{}
	f.V = f
	p.VisitWith(f)
	return f.expr
}

func (v *targetFinder) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	if v.target != 0 {
		return
	}

	call, ok := n.Expr.(*ast.CallExpression)
	if !ok || len(call.ArgumentList) != 2 {
		return
	}

	_, ok = call.ArgumentList[0].Expr.(*ast.Identifier)
	if !ok {
		return
	}

	secondArg, ok := call.ArgumentList[1].Expr.(*ast.NumberLiteral)
	if !ok {
		return
	}

	if secondArg.Value > 50000 {
		v.target = int(secondArg.Value)
	}
}

func collectAliases(p *ast.Program) (map[string]struct{}, error) {
	decoderFinder := &decoderFunctionFinder{}
	decoderFinder.V = decoderFinder
	p.VisitWith(decoderFinder)

	aliases := make(map[string]struct{})

	if decoderFinder.decoderName != "" {
		aliases[decoderFinder.decoderName] = struct{}{}
	}

	if len(aliases) == 0 {
		return nil, fmt.Errorf("failed to detect decoder function (no self-reassigning function with offset subtraction found)")
	}

	collector := &aliasCollector{
		aliases: aliases,
	}
	collector.V = collector
	p.VisitWith(collector)
	return aliases, nil
}

type decoderFunctionFinder struct {
	ast.NoopVisitor
	decoderName string
}

func (v *decoderFunctionFinder) VisitStatement(n *ast.Statement) {
	n.VisitChildrenWith(v)
	if v.decoderName != "" {
		return
	}

	fnDecl, ok := n.Stmt.(*ast.FunctionDeclaration)
	if !ok || fnDecl.Function == nil || fnDecl.Function.Name == nil {
		return
	}

	fnName := fnDecl.Function.Name.Name
	if fnDecl.Function.Body == nil {
		return
	}

	if v.isDecoderFunction(fnDecl.Function.Body, fnName) {
		v.decoderName = fnName
	}
}

func (v *decoderFunctionFinder) isDecoderFunction(body *ast.BlockStatement, fnName string) bool {
	if body == nil {
		return false
	}

	hasSelfReassign := false
	hasOffsetSubtract := false

	checker := &decoderPatternChecker{
		fnName: fnName,
	}
	checker.V = checker
	body.VisitWith(checker)

	hasSelfReassign = checker.hasSelfReassign
	hasOffsetSubtract = checker.hasOffsetSubtract

	return hasSelfReassign && hasOffsetSubtract
}

type decoderPatternChecker struct {
	ast.NoopVisitor
	fnName            string
	hasSelfReassign   bool
	hasOffsetSubtract bool
}

func (v *decoderPatternChecker) VisitStatement(n *ast.Statement) {
	n.VisitChildrenWith(v)
}

func (v *decoderPatternChecker) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)

	assign, ok := n.Expr.(*ast.AssignExpression)
	if !ok || assign.Operator.String() != "=" {
		return
	}

	if left, ok := assign.Left.Expr.(*ast.Identifier); ok && left.Name == v.fnName {
		if fn, ok := assign.Right.Expr.(*ast.FunctionLiteral); ok {
			v.hasSelfReassign = true
			if fn.Body != nil {
				fn.Body.VisitWith(v)
			}
		}
	}

	if left, ok := assign.Left.Expr.(*ast.Identifier); ok {
		if binary, ok := assign.Right.Expr.(*ast.BinaryExpression); ok && binary.Operator.String() == "-" {
			if rightId, ok := binary.Left.Expr.(*ast.Identifier); ok && rightId.Name == left.Name {
				if num, ok := binary.Right.Expr.(*ast.NumberLiteral); ok {
					if num.Value > 50 && num.Value < 2000 {
						v.hasOffsetSubtract = true
					}
				}
			}
		}
	}
}

type aliasCollector struct {
	ast.NoopVisitor
	aliases map[string]struct{}
}

func (v *aliasCollector) VisitStatement(n *ast.Statement) {
	n.VisitChildrenWith(v)
}

func (v *aliasCollector) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)

	assign, ok := n.Expr.(*ast.AssignExpression)
	if !ok || assign.Operator.String() != "=" {
		return
	}

	left, ok := assign.Left.Expr.(*ast.Identifier)
	if !ok {
		return
	}

	rightExpr := unwrapSequenceTail(assign.Right.Expr)

	right, ok := rightExpr.(*ast.Identifier)
	if !ok {
		return
	}

	if _, exists := v.aliases[right.Name]; exists {
		v.aliases[left.Name] = struct{}{}
	}
}

func buildstringsDynamic(p *ast.Program, offset, target int, rotationExpr ast.Expression, aliases map[string]struct{}) map[float64]string {
	raw := extractStringTable(p)
	if raw == "" {
		return nil
	}

	table := strings.Split(raw, ",")

	wkMap := extractWKMap(p, offset, len(table))

	table = rotateTableDynamic(table, offset, target, wkMap, rotationExpr, aliases)

	m := make(map[float64]string, len(table))
	for idx, val := range table {
		m[float64(idx+offset)] = val
	}
	return m
}

func extractWKMap(p *ast.Program, offset, tableLen int) map[string]int {
	finder := &wkMapFinder{
		offset:   offset,
		tableLen: tableLen,
	}
	finder.V = finder
	p.VisitWith(finder)
	return finder.wkMap
}

type wkMapFinder struct {
	ast.NoopVisitor
	wkMap    map[string]int
	offset   int
	tableLen int
}

func (v *wkMapFinder) VisitStatement(n *ast.Statement) {
	n.VisitChildrenWith(v)
}

func (v *wkMapFinder) tryExtractWK(obj *ast.ObjectLiteral) {
	wkMap := make(map[string]int)
	for _, entry := range obj.Value {
		prop, ok := entry.Prop.(*ast.PropertyKeyed)
		if !ok {
			continue
		}

		var keyName string
		switch key := prop.Key.Expr.(type) {
		case *ast.Identifier:
			keyName = key.Name
		case *ast.StringLiteral:
			keyName = key.Value
		default:
			continue
		}

		valNum, ok := prop.Value.Expr.(*ast.NumberLiteral)
		if !ok {
			continue
		}
		wkMap[keyName] = int(valNum.Value)
	}

	maxIdx := v.offset + v.tableLen
	allValid := true
	for _, val := range wkMap {
		if val < v.offset || val >= maxIdx {
			allValid = false
			break
		}
	}

	if !allValid {
		return
	}

	if v.wkMap == nil && len(wkMap) >= 9 && len(wkMap) <= 13 {
		v.wkMap = wkMap
	}
}

func (v *wkMapFinder) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)

	if obj, ok := n.Expr.(*ast.ObjectLiteral); ok {
		v.tryExtractWK(obj)
		return
	}

	assign, ok := n.Expr.(*ast.AssignExpression)
	if !ok {
		return
	}

	obj, ok := assign.Right.Expr.(*ast.ObjectLiteral)
	if !ok {
		return
	}

	v.tryExtractWK(obj)
}

func extractStringTable(p *ast.Program) string {
	finder := &stringTableFinder{}
	finder.V = finder
	p.VisitWith(finder)
	return finder.value
}

type stringTableFinder struct {
	ast.NoopVisitor
	value string
}

func (v *stringTableFinder) VisitExpression(n *ast.Expression) {
	n.VisitChildrenWith(v)
	if v.value != "" {
		return
	}

	call, ok := n.Expr.(*ast.CallExpression)
	if !ok || len(call.ArgumentList) != 1 {
		return
	}

	member, ok := call.Callee.Expr.(*ast.MemberExpression)
	if !ok {
		return
	}

	objectLit, ok := member.Object.Expr.(*ast.StringLiteral)
	if !ok {
		return
	}

	prop, ok := member.Property.Prop.(*ast.Identifier)
	if !ok || prop.Name != "split" {
		return
	}

	arg, ok := call.ArgumentList[0].Expr.(*ast.StringLiteral)
	if !ok || arg.Value != "," {
		return
	}

	if len(objectLit.Value) > len(v.value) {
		v.value = objectLit.Value
	}
}

func rotateTableDynamic(table []string, offset, target int, wkMap map[string]int, rotationExpr ast.Expression, aliases map[string]struct{}) []string {
	const maxCycles = 20000

	val := func(idx int) float64 {
		pos := idx - offset
		if pos < 0 || pos >= len(table) {
			return 0
		}
		return jsParseInt(table[pos])
	}

	for i := 0; i < maxCycles; i++ {
		sum := 0.0

		if rotationExpr.Expr != nil {
			sum = evalRotationExpr(rotationExpr.Expr, wkMap, aliases, val)
		}

		if int(math.Round(sum)) == target {
			break
		}

		if len(table) > 0 {
			table = append(table[1:], table[0])
		}
	}

	return table
}

func evalRotationExpr(expr ast.Node, wkMap map[string]int, aliases map[string]struct{}, val func(int) float64) float64 {
	switch e := expr.(type) {
	case *ast.Expression:
		return evalRotationExpr(e.Expr, wkMap, aliases, val)
	case *ast.NumberLiteral:
		return e.Value
	case *ast.UnaryExpression:
		switch e.Operator.String() {
		case "-":
			return -evalRotationExpr(e.Operand, wkMap, aliases, val)
		case "+":
			return evalRotationExpr(e.Operand, wkMap, aliases, val)
		default:
			return 0
		}
	case *ast.BinaryExpression:
		l := evalRotationExpr(e.Left, wkMap, aliases, val)
		r := evalRotationExpr(e.Right, wkMap, aliases, val)
		switch e.Operator.String() {
		case "+":
			return l + r
		case "-":
			return l - r
		case "*":
			return l * r
		case "/":
			if r != 0 {
				return l / r
			}
			return 0
		default:
			return 0
		}
	case *ast.CallExpression:
		if id, ok := e.Callee.Expr.(*ast.Identifier); ok && id.Name == "parseInt" && len(e.ArgumentList) >= 1 {
			arg := evalRotationExpr(&e.ArgumentList[0], wkMap, aliases, val)
			return arg
		}

		if id, ok := e.Callee.Expr.(*ast.Identifier); ok {
			if _, exists := aliases[id.Name]; exists {
				if len(e.ArgumentList) == 1 {
					if idx := evalIndex(&e.ArgumentList[0], wkMap); idx != -1 {
						return val(idx)
					}
				}
			}
		}
		return 0
	case *ast.MemberExpression:
		return float64(evalIndexFromMember(e, wkMap))
	case *ast.Identifier:
		return 0
	default:
		return 0
	}
}

func evalIndex(n ast.Node, wkMap map[string]int) int {
	switch v := n.(type) {
	case *ast.Expression:
		return evalIndex(v.Expr, wkMap)
	case *ast.MemberExpression:
		return evalIndexFromMember(v, wkMap)
	case *ast.NumberLiteral:
		return int(v.Value)
	default:
		return -1
	}
}

func evalIndexFromMember(m *ast.MemberExpression, wkMap map[string]int) int {
	obj, ok := m.Object.Expr.(*ast.Identifier)
	if !ok || obj.Name != "WK" {
		return -1
	}
	propName, ok := memberPropName(m.Property)
	if !ok {
		return -1
	}
	return wkMap[propName]
}

func memberPropName(mp *ast.MemberProperty) (string, bool) {
	if mp == nil || mp.Prop == nil {
		return "", false
	}
	switch p := mp.Prop.(type) {
	case *ast.Identifier:
		return p.Name, true
	case *ast.ComputedProperty:
		if p.Expr == nil {
			return "", false
		}
		switch key := p.Expr.Expr.(type) {
		case *ast.StringLiteral:
			return key.Value, true
		case *ast.Identifier:
			return "", false
		default:
			return "", false
		}
	default:
		return "", false
	}
}

func unwrapSequenceTail(expr ast.Expr) ast.Expr {
	for {
		seq, ok := expr.(*ast.SequenceExpression)
		if !ok || len(seq.Sequence) == 0 {
			return expr
		}
		expr = seq.Sequence[len(seq.Sequence)-1].Expr
	}
}

func jsParseInt(val string) float64 {
	if val == "" {
		return 0
	}

	sign := 1
	if val[0] == '-' {
		sign = -1
		val = val[1:]
	}

	result := 0
	for i := 0; i < len(val); i++ {
		if val[i] < '0' || val[i] > '9' {
			break
		}
		result = result*10 + int(val[i]-'0')
	}

	return float64(sign * result)
}

func (v *deobVisitor) isAlias(name string) bool {
	_, ok := v.aliases[name]
	return ok
}
