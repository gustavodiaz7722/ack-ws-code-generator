// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package code

import (
	"fmt"
	"strings"

	"github.com/aws-controllers-k8s/code-generator/pkg/fieldpath"
	"github.com/aws-controllers-k8s/code-generator/pkg/model"
	"github.com/samber/lo"
)

var (
	// iterVarFmt stores the format string which takes an integer and creates
	// the name of a variable used for the iterator part of a for-each loop
	iterVarFmt = "f%diter"

	// indexVarFmt stores the format string which takes an integer and creates
	// the name of a variable used for the index part of a for-each loop
	indexVarFmt = "f%didx"
)

// ReferenceFieldsValidation returns the go code to validate reference field and
// corresponding identifier field. Iterates through all references within
// slices, if necessary.
//
// Sample output (list of references):
//
//	for _, iter0 := range ko.Spec.Routes {
//	  if iter0.GatewayRef != nil && iter0.GatewayID != nil {
//	    return ackerr.ResourceReferenceAndIDNotSupportedFor("Routes.GatewayID", "Routes.GatewayRef")
//	  }
//	}
//
// Sample output (a single, required reference):
//
//	if ko.Spec.APIRef != nil && ko.Spec.APIID != nil {
//	  return ackerr.ResourceReferenceAndIDNotSupportedFor("APIID", "APIRef")
//	}
//
//	if ko.Spec.APIRef == nil && ko.Spec.APIID == nil {
//	  return ackerr.ResourceReferenceOrIDRequiredFor("APIID", "APIRef")
//	}
func ReferenceFieldsValidation(
	field *model.Field,
	sourceVarName string,
	indentLevel int,
) (string, error) {
	isListOfRefs := field.ShapeRef.Shape.Type == "list"

	refFieldName, err := field.GetReferenceFieldName()
	if err != nil {
		return "", err
	}
	refFieldPath, err := field.ReferenceFieldPath()
	if err != nil {
		return "", err
	}

	out, err := iterReferenceValues(field, indentLevel, sourceVarName, false,
		func(fieldAccessPrefix string, _, innerIndentLevel int) (innerOut string, err error) {
			innerIndent := strings.Repeat("\t", innerIndentLevel)

			// Get parent field path, in order to get to both the refs and concretes
			parentFP := fieldpath.FromString(fieldAccessPrefix)
			parentFP.Pop()

			// Validation to make sure both target field and reference are
			// not present at the same time in desired resource
			if isListOfRefs {
				innerOut += fmt.Sprintf("%sif len(%s.%s) > 0"+
					" && len(%s.%s) > 0 {\n", innerIndent, parentFP.String(), refFieldName.Camel, parentFP.String(), field.Names.Camel)
			} else {
				innerOut += fmt.Sprintf("%sif %s.%s != nil"+
					" && %s.%s != nil {\n", innerIndent, parentFP.String(), refFieldName.Camel, parentFP.String(), field.Names.Camel)
			}
			innerOut += fmt.Sprintf("%s\treturn "+
				"ackerr.ResourceReferenceAndIDNotSupportedFor(%q, %q)\n",
				innerIndent, field.Path, refFieldPath)
			innerOut += fmt.Sprintf("%s}\n", innerIndent)

			// If the field is required, make sure either Ref or original
			// field is present in the resource
			if field.IsRequired() {
				if isListOfRefs {
					innerOut += fmt.Sprintf("%sif len(%s.%s) == 0 &&"+
						" len(%s.%s) == 0 {\n", innerIndent, parentFP.String(),
						refFieldName.Camel, parentFP.String(), field.Names.Camel)
				} else {
					innerOut += fmt.Sprintf("%sif %s.%s == nil &&"+
						" %s.%s == nil {\n", innerIndent, parentFP.String(),
						refFieldName.Camel, parentFP.String(), field.Names.Camel)
				}
				innerOut += fmt.Sprintf("%s\treturn "+
					"ackerr.ResourceReferenceOrIDRequiredFor(%q, %q)\n",
					innerIndent, field.Names.Camel, refFieldName.Camel)
				innerOut += fmt.Sprintf("%s}\n", innerIndent)
			}

			return innerOut, nil
		})
	if err != nil {
		return "", err
	}

	return out, nil
}

// ResolveReferencesForField returns Go code for accessing all references that
// are related to the given concrete field, determining whether its in a valid
// condition and updating the concrete field with the referenced value.
//
// The generated code calls ackrt.ResolveCrossNamespaceReference to validate
// the reference and, when the reference targets a different namespace and
// the cross-namespace flag is enabled, emit a deprecation warning and set
// the ACK.CrossNamespaceOptInRequired condition on the resource. When the
// flag is disabled, the helper returns a terminal error.
//
// Sample output (resolving a singular reference):
//
//	if ko.Spec.APIRef != nil && ko.Spec.APIRef.From != nil {
//		hasReferences = true
//		arr := ko.Spec.APIRef.From
//		if arr.Name == nil || *arr.Name == "" {
//			return hasReferences, fmt.Errorf("provided resource reference is nil or empty: APIRef")
//		}
//		namespace, err := ackrt.ResolveCrossNamespaceReference(
//			ctx,
//			rm.cfg.EnableCrossNamespace,
//			&ko.Status.Conditions,
//			ackrt.CrossNamespaceRefKindResource,
//			ko.ObjectMeta.GetNamespace(),
//			arr.Namespace,
//			*arr.Name,
//		)
//		if err != nil {
//			return hasReferences, err
//		}
//		obj := &svcapitypes.API{}
//		if err := getReferencedResourceState_API(ctx, apiReader, obj, *arr.Name, namespace); err != nil {
//			return hasReferences, err
//		}
//		ko.Spec.APIID = (*string)(obj.Status.APIID)
//	}
//
// Sample output (resolving a list of references):
//
//	for _, f0iter := range ko.Spec.SecurityGroupRefs {
//		if f0iter != nil && f0iter.From != nil {
//			hasReferences = true
//			arr := f0iter.From
//			if arr.Name == nil || *arr.Name == "" {
//				return hasReferences, fmt.Errorf("provided resource reference is nil or empty: SecurityGroupRefs")
//			}
//			namespace, err := ackrt.ResolveCrossNamespaceReference( ... )
//			if err != nil {
//				return hasReferences, err
//			}
//			obj := &ec2apitypes.SecurityGroup{}
//			if err := getReferencedResourceState_SecurityGroup(ctx, apiReader, obj, *arr.Name, namespace); err != nil {
//				return hasReferences, err
//			}
//			if ko.Spec.SecurityGroupIDs == nil {
//				ko.Spec.SecurityGroupIDs = make([]*string, 0, 1)
//			}
//			ko.Spec.SecurityGroupIDs = append(ko.Spec.SecurityGroupIDs, (*string)(obj.Status.ID))
//		}
//	}
//
// Sample output (resolving nested lists of structs containing references):
//
//	if ko.Spec.Notification != nil {
//		for f0idx, f0iter := range ko.Spec.Notification.LambdaFunctionConfigurations {
//			if f0iter.Filter != nil {
//				if f0iter.Filter.Key != nil {
//					for f1idx, f1iter := range f0iter.Filter.Key.FilterRules {
//						if f1iter.ValueRef != nil && f1iter.ValueRef.From != nil {
//							hasReferences = true
//							arr := f1iter.ValueRef.From
//							if arr.Name == nil || *arr.Name == "" {
//								return hasReferences, fmt.Errorf("provided resource reference is nil or empty: Notification.LambdaFunctionConfigurations.Filter.Key.FilterRules.ValueRef")
//							}
//							namespace, err := ackrt.ResolveCrossNamespaceReference( ... )
//							if err != nil {
//								return hasReferences, err
//							}
//							obj := &svcapitypes.Bucket{}
//							if err := getReferencedResourceState_Bucket(ctx, apiReader, obj, *arr.Name, namespace); err != nil {
//								return hasReferences, err
//							}
//							ko.Spec.Notification.LambdaFunctionConfigurations[f0idx].Filter.Key.FilterRules[f1idx].Value = (*string)(obj.Spec.Name)
//						}
//					}
//				}
//			}
//		}
//	}
func ResolveReferencesForField(field *model.Field, sourceVarName string, indentLevel int) (string, error) {
	isListOfRefs := field.ShapeRef.Shape.Type == "list"

	resRefElemType := field.ShapeRef.GoType()
	if isListOfRefs {
		resRefElemType = field.ShapeRef.Shape.MemberRef.GoType()
	}

	refFieldPath, err := field.ReferenceFieldPath()
	if err != nil {
		return "", err
	}

	iterOut, err := iterReferenceValues(field, indentLevel, sourceVarName, true,
		func(fieldAccessPrefix string, listDepth, innerIndentLevel int) (string, error) {
			innerIndent := strings.Repeat("\t", innerIndentLevel)

			outPrefix := ""
			outSuffix := ""

			// If the reference field is a list of primitives, iterate through each.
			// We need to duplicate some code from above, here, because `*Ref` fields
			// aren't registered as fields, so we can't use the same common logic
			if isListOfRefs {
				iterVarName := fmt.Sprintf(iterVarFmt, listDepth)

				outPrefix += fmt.Sprintf("%sfor _, %s := range %s {\n", strings.Repeat("\t", innerIndentLevel), iterVarName, fieldAccessPrefix)
				outSuffix = fmt.Sprintf("%s}\n%s", strings.Repeat("\t", innerIndentLevel), outSuffix)
				fieldAccessPrefix = iterVarName

				innerIndentLevel++
				innerIndent = strings.Repeat("\t", innerIndentLevel)
			}

			outPrefix += fmt.Sprintf("%sif %s != nil && %s.From != nil {\n", strings.Repeat("\t", innerIndentLevel), fieldAccessPrefix, fieldAccessPrefix)
			outSuffix = fmt.Sprintf("%s}\n%s", strings.Repeat("\t", innerIndentLevel), outSuffix)

			innerIndentLevel++
			innerIndent = strings.Repeat("\t", innerIndentLevel)

			outPrefix += fmt.Sprintf("%shasReferences = true\n", innerIndent)

			outPrefix += fmt.Sprintf("%sarr := %s.From\n", innerIndent, fieldAccessPrefix)
			outPrefix += fmt.Sprintf("%sif arr.Name == nil || *arr.Name == \"\" {\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\treturn hasReferences, fmt.Errorf(\"provided resource reference is nil or empty: %s\")\n", innerIndent, refFieldPath)
			outPrefix += fmt.Sprintf("%s}\n", innerIndent)

			outPrefix += fmt.Sprintf("%snamespace, err := ackrt.ResolveCrossNamespaceReference(\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\tctx,\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\trm.cfg.EnableCrossNamespace,\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\t&ko.Status.Conditions,\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\tackrt.CrossNamespaceRefKindResource,\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\tko.ObjectMeta.GetNamespace(),\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\tarr.Namespace,\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\t*arr.Name,\n", innerIndent)
			outPrefix += fmt.Sprintf("%s)\n", innerIndent)
			outPrefix += fmt.Sprintf("%sif err != nil {\n", innerIndent)
			outPrefix += fmt.Sprintf("%s\treturn hasReferences, err\n", innerIndent)
			outPrefix += fmt.Sprintf("%s}\n", innerIndent)

			outPrefix += getReferencedStateForField(field, innerIndentLevel)

			concreteValueAccessor, err := buildIndexBasedFieldAccessor(field, sourceVarName, indexVarFmt)
			if err != nil {
				return "", err
			}
			if isListOfRefs {
				outPrefix += fmt.Sprintf("%sif %s == nil {\n", innerIndent, concreteValueAccessor)
				outPrefix += fmt.Sprintf("%s\t%s = make([]%s, 0, 1)\n", innerIndent, concreteValueAccessor, resRefElemType)
				outPrefix += fmt.Sprintf("%s}\n", innerIndent)
				outPrefix += fmt.Sprintf("%s%s = append(%s, (%s)(obj.%s))\n", innerIndent, concreteValueAccessor, concreteValueAccessor, resRefElemType, field.FieldConfig.References.Path)
			} else {
				outPrefix += fmt.Sprintf("%s%s = (%s)(obj.%s)\n", innerIndent, concreteValueAccessor, resRefElemType, field.FieldConfig.References.Path)
			}

			return outPrefix + outSuffix, nil
		})
	if err != nil {
		return "", err
	}

	return iterOut, nil
}

// ClearResolvedReferencesForField returns Go code that iterates over all
// references within an AWSResource and, if the reference is non-nil, sets the
// respective concrete value to nil.
//
// Sample output:
//
//	for f0idx, f0iter := range ko.Spec.Routes {
//		if f0iter.GatewayRef != nil {
//			ko.Spec.Routes[f0idx].GatewayID = nil
//		}
//	}
func ClearResolvedReferencesForField(field *model.Field, targetVarName string, indentLevel int) (string, error) {
	isListOfRefs := field.ShapeRef.Shape.Type == "list"

	iterOut, err := iterReferenceValues(field, indentLevel, targetVarName, true,
		func(fieldAccessPrefix string, _, innerIndentLevel int) (innerOut string, err error) {
			innerIndent := strings.Repeat("\t", innerIndentLevel)

			// If we are dealing with a list of references, then we don't need to
			// iterate over all of the references individually. We know that if the list
			// has >0 elements, then the entire concrete value list should be made nil.
			// To deal with this, we should iterate only to the parent of the list and
			// then check these conditions.
			if isListOfRefs {
				innerOut += fmt.Sprintf("%sif len(%s) > 0 {\n", innerIndent, fieldAccessPrefix)
			} else {
				innerOut += fmt.Sprintf("%sif %s != nil {\n", innerIndent, fieldAccessPrefix)
			}
			concreteValueAccessor, err := buildIndexBasedFieldAccessor(field, targetVarName, indexVarFmt)
			if err != nil {
				return "", err
			}
			innerOut += fmt.Sprintf("%s\t%s = nil\n", innerIndent, concreteValueAccessor)
			innerOut += fmt.Sprintf("%s}\n", innerIndent)

			return innerOut, nil
		})
	if err != nil {
		return "", err
	}

	return iterOut, nil
}

// iterReferenceValues returns Go code that drills down through the spec, doing
// nil checks and iterating over values, until it reaches the reference field
// for the given field. Once it reaches the reference field, it runs the inner
// render callback. It returns the concatenation of the drilling code and the
// inner render return value.
//
// The inner render callback is passed a `fieldAccessPrefix`, which is the name
// of a variable which can be used to access the ref field within the nested
// lists and structs, a `listDepth`, which represents the number of lists that
// the ref field is inside, and an `indentLevel` which represents the layers of
// indentation the code has reached.
func iterReferenceValues(
	field *model.Field,
	indentLevel int,
	sourceVarName string,
	shouldRenderIndexes bool,
	innerRender func(fieldAccessPrefix string, listDepth, indentLevel int) (innerOut string, err error),
) (string, error) {
	r := field.CRD
	refFieldPath, err := field.ReferenceFieldPath()
	if err != nil {
		return "", err
	}
	fp := fieldpath.FromString(refFieldPath)

	refFieldName, err := field.GetReferenceFieldName()
	if err != nil {
		return "", err
	}

	outPrefix := ""
	outSuffix := ""

	currentListDepth := 0 // Stores how many slices we are iterating within

	fieldAccessPrefix := fmt.Sprintf("%s%s", sourceVarName, r.Config().PrefixConfig.SpecField)

	fpDepth := 0
	for fpDepth = 0; fpDepth < fp.Size()-1; fpDepth++ {
		curFP := fp.CopyAt(fpDepth).String()
		cur, ok := r.Fields[curFP]
		if !ok {
			return "", fmt.Errorf(
				"resource %q: unable to find field with path %q",
				r.Kind, curFP,
			)
		}

		ref := cur.ShapeRef

		indent := strings.Repeat("\t", indentLevel+fpDepth)

		switch ref.Shape.Type {
		case ("map"):
			return "", fmt.Errorf(
				"resource %q, field %q: references cannot be within a map",
				r.Kind, field.Path,
			)
		case ("structure"):
			fieldAccessPrefix = fmt.Sprintf("%s.%s", fieldAccessPrefix, fp.At(fpDepth))

			outPrefix += fmt.Sprintf("%sif %s != nil {\n", indent, fieldAccessPrefix)
			outSuffix = fmt.Sprintf("%s}\n%s", indent, outSuffix)
		case ("list"):
			iterVarName := fmt.Sprintf(iterVarFmt, currentListDepth)
			idxVarName := fmt.Sprintf(indexVarFmt, currentListDepth)

			fieldAccessPrefix = fmt.Sprintf("%s.%s", fieldAccessPrefix, fp.At(fpDepth))

			outPrefix += fmt.Sprintf("%sfor %s, %s := range %s {\n", indent,
				lo.Ternary(shouldRenderIndexes, idxVarName, "_"),
				iterVarName,
				fieldAccessPrefix,
			)

			outSuffix = fmt.Sprintf("%s}\n%s", indent, outSuffix)

			fieldAccessPrefix = iterVarName

			currentListDepth++
		}
	}

	fieldAccessPrefix = fmt.Sprintf("%s.%s", fieldAccessPrefix, refFieldName.Camel)
	innerIndentLevel := indentLevel + fpDepth

	innerPrefix, err := innerRender(fieldAccessPrefix, currentListDepth, innerIndentLevel)
	if err != nil {
		return "", err
	}

	return outPrefix + innerPrefix + outSuffix, nil
}

// buildNestedFieldAccessor generates Go code that accesses an inner struct,
// using slice indexes where necessary.
//
// `indexVarFmt` should be a format string that takes a single integer and
// returns the name of a variable which holds the index for the n-th parent
// slice. For example, f%didx will be used to create f0idx, f1idx, etc. for the
// parent slices in the accessors.
//
// By default, this method will iterate through every field in the field path.
// Supplying a `parentOffset` will only iterate through the first `fp.Size() -
// parentOffset` number of paths.
func buildIndexBasedFieldAccessorWithOffset(field *model.Field, sourceVarName, indexVarFmt string, parentOffset int) (string, error) {
	r := field.CRD
	fp := fieldpath.FromString(field.Path)

	isList := field.ShapeRef.Shape.Type == "list"

	fieldNamePrefix := ""
	nestedFieldDepth := 0
	for idx := 0; idx < fp.Size()-parentOffset; idx++ {
		curFP := fp.CopyAt(idx)

		cur, ok := r.Fields[curFP.String()]
		if !ok {
			return "", fmt.Errorf("unable to find field with path %q. crd: %q", curFP.String(), r.Kind)
		}

		fieldName := curFP.Pop()
		indexList := ""

		if cur.ShapeRef.Shape.Type == "list" {

			// We want to access indexes when iterating through lists of
			// structs. If we find a list at the end of the field path, then we
			// know the initial field must be a list. We want to pass that back
			// as a full array, rather than accessing the individual values.
			// This only applies for when there is no offset, since any offset >
			// 0 will cut off the initial field from the path
			if idx != (fp.Size()-1) && !isList {
				indexList = fmt.Sprintf("[%s]", fmt.Sprintf(indexVarFmt, nestedFieldDepth))
				nestedFieldDepth++
			}
		}

		fieldNamePrefix = fmt.Sprintf("%s.%s%s", fieldNamePrefix, fieldName, indexList)
	}

	return fmt.Sprintf("%s%s%s", sourceVarName, r.Config().PrefixConfig.SpecField, fieldNamePrefix), nil
}

// buildIndexBasedFieldAccessor calls buildNestedFieldAccessorWithOffset with an
// offset of 0.
func buildIndexBasedFieldAccessor(field *model.Field, sourceVarName, indexVarFmt string) (string, error) {
	return buildIndexBasedFieldAccessorWithOffset(field, sourceVarName, indexVarFmt, 0)
}

// getReferencedStateForField returns Go code that makes a call to
// `getReferencedResourceState_*` (using the referenced field resource) and sets
// the response into an object (of the referenced type) called `obj`
func getReferencedStateForField(field *model.Field, indentLevel int) string {
	out := ""
	indent := strings.Repeat("\t", indentLevel)

	if field.FieldConfig.References.ServiceName == "" {
		out += fmt.Sprintf("%sobj := &svcapitypes.%s{}\n", indent, field.FieldConfig.References.Resource)
	} else {
		out += fmt.Sprintf("%sobj := &%sapitypes.%s{}\n", indent, field.ReferencedServiceName(), field.FieldConfig.References.Resource)
	}
	out += fmt.Sprintf("%sif err := getReferencedResourceState_%s(ctx, apiReader, obj, *arr.Name, namespace); err != nil {\n", indent, field.FieldConfig.References.Resource)
	out += fmt.Sprintf("%s\treturn hasReferences, err\n", indent)
	out += fmt.Sprintf("%s}\n", indent)

	return out
}

// hasCollectionAncestor reports whether a collection lies on the path to a field's
// reference (`*Ref`) sibling.
//
// It returns true for a list. A map ancestor is rejected with an error instead: a
// reference cannot be addressed through a map at all, so there is nothing sensible
// to generate.
//
// Only ancestors count. A reference field that is itself a list (`*Refs`, whose
// concrete sibling is a list of scalars) is the leaf rather than part of the path,
// so nothing has to be indexed to reach it.
//
// EnsureReferences uses this to skip such a reference; see its doc comment.
func hasCollectionAncestor(field *model.Field) (bool, error) {
	r := field.CRD
	refFieldPath, err := field.ReferenceFieldPath()
	if err != nil {
		return false, err
	}
	fp := fieldpath.FromString(refFieldPath)
	for depth := 0; depth < fp.Size()-1; depth++ {
		curFP := fp.CopyAt(depth).String()
		cur, ok := r.Fields[curFP]
		if !ok {
			return false, fmt.Errorf(
				"resource %q: unable to find field with path %q", r.Kind, curFP,
			)
		}
		if cur.ShapeRef.Shape.Type == "map" {
			return false, fmt.Errorf(
				"resource %q, field %q: references cannot be within a map",
				r.Kind, field.Path,
			)
		}
		if cur.ShapeRef.Shape.Type == "list" {
			return true, nil
		}
	}
	return false, nil
}

// EnsureReferences returns Go code that restores, from a source object into a
// target object, the cross-resource reference (`*Ref`) fields the target is
// missing.
//
// A `*Ref` is a sibling of the concrete field it resolves into. A resource manager
// builds its return value from an AWS API response, which has no concept of a
// reference, so rebuilding the containing struct drops every `*Ref` inside it.
// That disables ClearResolvedReferences, which suppresses a resolved value only
// while the sibling `*Ref` is visible, so the spec patch deletes the declared
// `*Ref` and stores the resolved value in its place. The next apply of the
// manifest puts the `*Ref` back beside that value, a pair
// validateReferenceFields rejects, stopping reconciliation. See
// aws-controllers-k8s/community#2361 and #2431.
//
// Only a reference reached through STRUCTS is emitted. It has one fixed address,
// so exactly that field is assigned and every value the service reported stands.
//
// A TOP-LEVEL reference is skipped: generated set-output code starts from a
// DeepCopy of the object it was handed and overwrites only the concrete field, and
// the `*Ref` is a sibling of that field rather than part of it, so nothing rebuilds
// it. That holds for the generated paths; a hand-written set-output hook that
// rebuilds the object wholesale could still drop it, in which case the hook has to
// carry the reference across itself.
//
// A reference reached through a LIST is also skipped, and behaves as it does today.
// It has no fixed address, so restoring it means pairing an element the service
// reported with an element the user declared, and neither available key is sound:
//
//   - Position is not reliable, because an AWS response need not preserve the order
//     of the request.
//   - The resolved value is not reliable either, because it is not always unique.
//     A reference resolves to whatever path `references.path` names, and while most
//     name an AWS-assigned identifier, 75 of the roughly 470 references configured
//     across the controllers resolve to a `Spec.*` path that carries no uniqueness
//     guarantee. `sqs/Queue.Policy` and `sns/Topic.Policy`, for instance, resolve
//     `iam/Policy` via `Spec.PolicyDocument` -- the policy document itself -- so two
//     separate IAM policies granting the same thing resolve to the same value.
//
// Replacing the whole outermost list avoids having to pair anything, but discards
// whatever the service populated inside it, which for an element carrying
// AWS-assigned members (ec2's `NetworkACL.Associations`) means losing them from the
// stored spec.
//
// A sound per-element restore needs a declared notion of element identity -- a set
// of fields named in `generator.yaml` that uniquely identify an entry -- which is
// left to a follow-up.
//
// Sample output:
//
//	if desiredKO.Spec.JWTConfiguration != nil && latestKO.Spec.JWTConfiguration != nil && desiredKO.Spec.JWTConfiguration.IssuerRef != nil && latestKO.Spec.JWTConfiguration.IssuerRef == nil {
//		latestKO.Spec.JWTConfiguration.IssuerRef = desiredKO.Spec.JWTConfiguration.IssuerRef
//	}
func EnsureReferences(
	r *model.CRD,
	sourceVarName string,
	targetVarName string,
	indentLevel int,
) (string, error) {
	out := ""
	indent := strings.Repeat("\t", indentLevel)
	specField := r.Config().PrefixConfig.SpecField

	for _, fieldName := range r.SortedFieldNames() {
		field := r.Fields[fieldName]
		if !field.HasReference() {
			continue
		}
		refName, err := field.GetReferenceFieldName()
		if err != nil {
			return "", err
		}
		refFieldPath, err := field.ReferenceFieldPath()
		if err != nil {
			return "", err
		}
		fp := fieldpath.FromString(refFieldPath)

		// A top-level reference has no parent to be rebuilt.
		if fp.Size() < 2 {
			continue
		}

		// A reference behind a list has no fixed address to assign to; see the doc
		// comment.
		inList, err := hasCollectionAncestor(field)
		if err != nil {
			return "", err
		}
		if inList {
			continue
		}

		// Struct-only path: guard every ancestor on both objects, then assign
		// just the reference when the target lacks it.
		srcAccess := sourceVarName + specField
		tgtAccess := targetVarName + specField
		conds := make([]string, 0, fp.Size()*2)
		for depth := 0; depth < fp.Size()-1; depth++ {
			srcAccess = fmt.Sprintf("%s.%s", srcAccess, fp.At(depth))
			tgtAccess = fmt.Sprintf("%s.%s", tgtAccess, fp.At(depth))
			conds = append(conds, fmt.Sprintf("%s != nil", srcAccess))
			conds = append(conds, fmt.Sprintf("%s != nil", tgtAccess))
		}
		srcRef := fmt.Sprintf("%s.%s", srcAccess, refName.Camel)
		tgtRef := fmt.Sprintf("%s.%s", tgtAccess, refName.Camel)
		if field.ShapeRef.Shape.Type == "list" {
			// A list-of-references field is one value at a fixed address, so it
			// is copied whole; length stands in for nil, as it does in
			// ClearResolvedReferences for the same shape.
			conds = append(conds, fmt.Sprintf("len(%s) > 0", srcRef))
			conds = append(conds, fmt.Sprintf("len(%s) == 0", tgtRef))
		} else {
			conds = append(conds, fmt.Sprintf("%s != nil", srcRef))
			conds = append(conds, fmt.Sprintf("%s == nil", tgtRef))
		}
		out += fmt.Sprintf("%sif %s {\n", indent, strings.Join(conds, " && "))
		out += fmt.Sprintf("%s\t%s = %s\n", indent, tgtRef, srcRef)
		out += fmt.Sprintf("%s}\n", indent)
	}

	return out, nil
}
