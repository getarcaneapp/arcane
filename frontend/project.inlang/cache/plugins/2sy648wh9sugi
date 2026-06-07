var __create = Object.create;
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __getProtoOf = Object.getPrototypeOf;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __commonJS = (cb, mod) => function __require() {
  return mod || (0, cb[__getOwnPropNames(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
  // If the importer is in node compatibility mode or this is not an ESM
  // file that has been converted to a CommonJS file using a Babel-
  // compatible transform (i.e. "__esModule" has not been set), then set
  // "default" to the CommonJS "module.exports" for node compatibility.
  isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
  mod
));

// ../../../node_modules/.pnpm/@sinclair+typebox@0.31.28/node_modules/@sinclair/typebox/typebox.js
var require_typebox = __commonJS({
  "../../../node_modules/.pnpm/@sinclair+typebox@0.31.28/node_modules/@sinclair/typebox/typebox.js"(exports) {
    "use strict";
    Object.defineProperty(exports, "__esModule", { value: true });
    exports.Type = exports.JsonType = exports.JavaScriptTypeBuilder = exports.JsonTypeBuilder = exports.TypeBuilder = exports.TypeBuilderError = exports.TransformEncodeBuilder = exports.TransformDecodeBuilder = exports.TemplateLiteralDslParser = exports.TemplateLiteralGenerator = exports.TemplateLiteralGeneratorError = exports.TemplateLiteralFinite = exports.TemplateLiteralFiniteError = exports.TemplateLiteralParser = exports.TemplateLiteralParserError = exports.TemplateLiteralResolver = exports.TemplateLiteralPattern = exports.TemplateLiteralPatternError = exports.UnionResolver = exports.KeyArrayResolver = exports.KeyArrayResolverError = exports.KeyResolver = exports.ObjectMap = exports.Intrinsic = exports.IndexedAccessor = exports.TypeClone = exports.TypeExtends = exports.TypeExtendsResult = exports.TypeExtendsError = exports.ExtendsUndefined = exports.TypeGuard = exports.TypeGuardUnknownTypeError = exports.ValueGuard = exports.FormatRegistry = exports.TypeBoxError = exports.TypeRegistry = exports.PatternStringExact = exports.PatternNumberExact = exports.PatternBooleanExact = exports.PatternString = exports.PatternNumber = exports.PatternBoolean = exports.Kind = exports.Hint = exports.Optional = exports.Readonly = exports.Transform = void 0;
    exports.Transform = Symbol.for("TypeBox.Transform");
    exports.Readonly = Symbol.for("TypeBox.Readonly");
    exports.Optional = Symbol.for("TypeBox.Optional");
    exports.Hint = Symbol.for("TypeBox.Hint");
    exports.Kind = Symbol.for("TypeBox.Kind");
    exports.PatternBoolean = "(true|false)";
    exports.PatternNumber = "(0|[1-9][0-9]*)";
    exports.PatternString = "(.*)";
    exports.PatternBooleanExact = `^${exports.PatternBoolean}$`;
    exports.PatternNumberExact = `^${exports.PatternNumber}$`;
    exports.PatternStringExact = `^${exports.PatternString}$`;
    var TypeRegistry;
    (function(TypeRegistry2) {
      const map = /* @__PURE__ */ new Map();
      function Entries() {
        return new Map(map);
      }
      TypeRegistry2.Entries = Entries;
      function Clear() {
        return map.clear();
      }
      TypeRegistry2.Clear = Clear;
      function Delete(kind) {
        return map.delete(kind);
      }
      TypeRegistry2.Delete = Delete;
      function Has(kind) {
        return map.has(kind);
      }
      TypeRegistry2.Has = Has;
      function Set2(kind, func) {
        map.set(kind, func);
      }
      TypeRegistry2.Set = Set2;
      function Get(kind) {
        return map.get(kind);
      }
      TypeRegistry2.Get = Get;
    })(TypeRegistry || (exports.TypeRegistry = TypeRegistry = {}));
    var TypeBoxError = class extends Error {
      constructor(message) {
        super(message);
      }
    };
    exports.TypeBoxError = TypeBoxError;
    var FormatRegistry;
    (function(FormatRegistry2) {
      const map = /* @__PURE__ */ new Map();
      function Entries() {
        return new Map(map);
      }
      FormatRegistry2.Entries = Entries;
      function Clear() {
        return map.clear();
      }
      FormatRegistry2.Clear = Clear;
      function Delete(format) {
        return map.delete(format);
      }
      FormatRegistry2.Delete = Delete;
      function Has(format) {
        return map.has(format);
      }
      FormatRegistry2.Has = Has;
      function Set2(format, func) {
        map.set(format, func);
      }
      FormatRegistry2.Set = Set2;
      function Get(format) {
        return map.get(format);
      }
      FormatRegistry2.Get = Get;
    })(FormatRegistry || (exports.FormatRegistry = FormatRegistry = {}));
    var ValueGuard;
    (function(ValueGuard2) {
      function IsArray(value) {
        return Array.isArray(value);
      }
      ValueGuard2.IsArray = IsArray;
      function IsBigInt(value) {
        return typeof value === "bigint";
      }
      ValueGuard2.IsBigInt = IsBigInt;
      function IsBoolean(value) {
        return typeof value === "boolean";
      }
      ValueGuard2.IsBoolean = IsBoolean;
      function IsDate(value) {
        return value instanceof globalThis.Date;
      }
      ValueGuard2.IsDate = IsDate;
      function IsNull(value) {
        return value === null;
      }
      ValueGuard2.IsNull = IsNull;
      function IsNumber(value) {
        return typeof value === "number";
      }
      ValueGuard2.IsNumber = IsNumber;
      function IsObject(value) {
        return typeof value === "object" && value !== null;
      }
      ValueGuard2.IsObject = IsObject;
      function IsString(value) {
        return typeof value === "string";
      }
      ValueGuard2.IsString = IsString;
      function IsUint8Array(value) {
        return value instanceof globalThis.Uint8Array;
      }
      ValueGuard2.IsUint8Array = IsUint8Array;
      function IsUndefined(value) {
        return value === void 0;
      }
      ValueGuard2.IsUndefined = IsUndefined;
    })(ValueGuard || (exports.ValueGuard = ValueGuard = {}));
    var TypeGuardUnknownTypeError = class extends TypeBoxError {
    };
    exports.TypeGuardUnknownTypeError = TypeGuardUnknownTypeError;
    var TypeGuard;
    (function(TypeGuard2) {
      function IsPattern(value) {
        try {
          new RegExp(value);
          return true;
        } catch {
          return false;
        }
      }
      function IsControlCharacterFree(value) {
        if (!ValueGuard.IsString(value))
          return false;
        for (let i = 0; i < value.length; i++) {
          const code = value.charCodeAt(i);
          if (code >= 7 && code <= 13 || code === 27 || code === 127) {
            return false;
          }
        }
        return true;
      }
      function IsAdditionalProperties(value) {
        return IsOptionalBoolean(value) || TSchema(value);
      }
      function IsOptionalBigInt(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsBigInt(value);
      }
      function IsOptionalNumber(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsNumber(value);
      }
      function IsOptionalBoolean(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsBoolean(value);
      }
      function IsOptionalString(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsString(value);
      }
      function IsOptionalPattern(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsString(value) && IsControlCharacterFree(value) && IsPattern(value);
      }
      function IsOptionalFormat(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsString(value) && IsControlCharacterFree(value);
      }
      function IsOptionalSchema(value) {
        return ValueGuard.IsUndefined(value) || TSchema(value);
      }
      function TAny(schema) {
        return TKindOf(schema, "Any") && IsOptionalString(schema.$id);
      }
      TypeGuard2.TAny = TAny;
      function TArray(schema) {
        return TKindOf(schema, "Array") && schema.type === "array" && IsOptionalString(schema.$id) && TSchema(schema.items) && IsOptionalNumber(schema.minItems) && IsOptionalNumber(schema.maxItems) && IsOptionalBoolean(schema.uniqueItems) && IsOptionalSchema(schema.contains) && IsOptionalNumber(schema.minContains) && IsOptionalNumber(schema.maxContains);
      }
      TypeGuard2.TArray = TArray;
      function TAsyncIterator(schema) {
        return TKindOf(schema, "AsyncIterator") && schema.type === "AsyncIterator" && IsOptionalString(schema.$id) && TSchema(schema.items);
      }
      TypeGuard2.TAsyncIterator = TAsyncIterator;
      function TBigInt(schema) {
        return TKindOf(schema, "BigInt") && schema.type === "bigint" && IsOptionalString(schema.$id) && IsOptionalBigInt(schema.exclusiveMaximum) && IsOptionalBigInt(schema.exclusiveMinimum) && IsOptionalBigInt(schema.maximum) && IsOptionalBigInt(schema.minimum) && IsOptionalBigInt(schema.multipleOf);
      }
      TypeGuard2.TBigInt = TBigInt;
      function TBoolean(schema) {
        return TKindOf(schema, "Boolean") && schema.type === "boolean" && IsOptionalString(schema.$id);
      }
      TypeGuard2.TBoolean = TBoolean;
      function TConstructor(schema) {
        return TKindOf(schema, "Constructor") && schema.type === "Constructor" && IsOptionalString(schema.$id) && ValueGuard.IsArray(schema.parameters) && schema.parameters.every((schema2) => TSchema(schema2)) && TSchema(schema.returns);
      }
      TypeGuard2.TConstructor = TConstructor;
      function TDate(schema) {
        return TKindOf(schema, "Date") && schema.type === "Date" && IsOptionalString(schema.$id) && IsOptionalNumber(schema.exclusiveMaximumTimestamp) && IsOptionalNumber(schema.exclusiveMinimumTimestamp) && IsOptionalNumber(schema.maximumTimestamp) && IsOptionalNumber(schema.minimumTimestamp) && IsOptionalNumber(schema.multipleOfTimestamp);
      }
      TypeGuard2.TDate = TDate;
      function TFunction(schema) {
        return TKindOf(schema, "Function") && schema.type === "Function" && IsOptionalString(schema.$id) && ValueGuard.IsArray(schema.parameters) && schema.parameters.every((schema2) => TSchema(schema2)) && TSchema(schema.returns);
      }
      TypeGuard2.TFunction = TFunction;
      function TInteger(schema) {
        return TKindOf(schema, "Integer") && schema.type === "integer" && IsOptionalString(schema.$id) && IsOptionalNumber(schema.exclusiveMaximum) && IsOptionalNumber(schema.exclusiveMinimum) && IsOptionalNumber(schema.maximum) && IsOptionalNumber(schema.minimum) && IsOptionalNumber(schema.multipleOf);
      }
      TypeGuard2.TInteger = TInteger;
      function TIntersect(schema) {
        return TKindOf(schema, "Intersect") && (ValueGuard.IsString(schema.type) && schema.type !== "object" ? false : true) && ValueGuard.IsArray(schema.allOf) && schema.allOf.every((schema2) => TSchema(schema2) && !TTransform(schema2)) && IsOptionalString(schema.type) && (IsOptionalBoolean(schema.unevaluatedProperties) || IsOptionalSchema(schema.unevaluatedProperties)) && IsOptionalString(schema.$id);
      }
      TypeGuard2.TIntersect = TIntersect;
      function TIterator(schema) {
        return TKindOf(schema, "Iterator") && schema.type === "Iterator" && IsOptionalString(schema.$id) && TSchema(schema.items);
      }
      TypeGuard2.TIterator = TIterator;
      function TKindOf(schema, kind) {
        return TKind(schema) && schema[exports.Kind] === kind;
      }
      TypeGuard2.TKindOf = TKindOf;
      function TKind(schema) {
        return ValueGuard.IsObject(schema) && exports.Kind in schema && ValueGuard.IsString(schema[exports.Kind]);
      }
      TypeGuard2.TKind = TKind;
      function TLiteralString(schema) {
        return TLiteral(schema) && ValueGuard.IsString(schema.const);
      }
      TypeGuard2.TLiteralString = TLiteralString;
      function TLiteralNumber(schema) {
        return TLiteral(schema) && ValueGuard.IsNumber(schema.const);
      }
      TypeGuard2.TLiteralNumber = TLiteralNumber;
      function TLiteralBoolean(schema) {
        return TLiteral(schema) && ValueGuard.IsBoolean(schema.const);
      }
      TypeGuard2.TLiteralBoolean = TLiteralBoolean;
      function TLiteral(schema) {
        return TKindOf(schema, "Literal") && IsOptionalString(schema.$id) && (ValueGuard.IsBoolean(schema.const) || ValueGuard.IsNumber(schema.const) || ValueGuard.IsString(schema.const));
      }
      TypeGuard2.TLiteral = TLiteral;
      function TNever(schema) {
        return TKindOf(schema, "Never") && ValueGuard.IsObject(schema.not) && Object.getOwnPropertyNames(schema.not).length === 0;
      }
      TypeGuard2.TNever = TNever;
      function TNot(schema) {
        return TKindOf(schema, "Not") && TSchema(schema.not);
      }
      TypeGuard2.TNot = TNot;
      function TNull(schema) {
        return TKindOf(schema, "Null") && schema.type === "null" && IsOptionalString(schema.$id);
      }
      TypeGuard2.TNull = TNull;
      function TNumber(schema) {
        return TKindOf(schema, "Number") && schema.type === "number" && IsOptionalString(schema.$id) && IsOptionalNumber(schema.exclusiveMaximum) && IsOptionalNumber(schema.exclusiveMinimum) && IsOptionalNumber(schema.maximum) && IsOptionalNumber(schema.minimum) && IsOptionalNumber(schema.multipleOf);
      }
      TypeGuard2.TNumber = TNumber;
      function TObject(schema) {
        return TKindOf(schema, "Object") && schema.type === "object" && IsOptionalString(schema.$id) && ValueGuard.IsObject(schema.properties) && IsAdditionalProperties(schema.additionalProperties) && IsOptionalNumber(schema.minProperties) && IsOptionalNumber(schema.maxProperties) && Object.entries(schema.properties).every(([key, schema2]) => IsControlCharacterFree(key) && TSchema(schema2));
      }
      TypeGuard2.TObject = TObject;
      function TPromise(schema) {
        return TKindOf(schema, "Promise") && schema.type === "Promise" && IsOptionalString(schema.$id) && TSchema(schema.item);
      }
      TypeGuard2.TPromise = TPromise;
      function TRecord(schema) {
        return TKindOf(schema, "Record") && schema.type === "object" && IsOptionalString(schema.$id) && IsAdditionalProperties(schema.additionalProperties) && ValueGuard.IsObject(schema.patternProperties) && ((schema2) => {
          const keys = Object.getOwnPropertyNames(schema2.patternProperties);
          return keys.length === 1 && IsPattern(keys[0]) && ValueGuard.IsObject(schema2.patternProperties) && TSchema(schema2.patternProperties[keys[0]]);
        })(schema);
      }
      TypeGuard2.TRecord = TRecord;
      function TRecursive(schema) {
        return ValueGuard.IsObject(schema) && exports.Hint in schema && schema[exports.Hint] === "Recursive";
      }
      TypeGuard2.TRecursive = TRecursive;
      function TRef(schema) {
        return TKindOf(schema, "Ref") && IsOptionalString(schema.$id) && ValueGuard.IsString(schema.$ref);
      }
      TypeGuard2.TRef = TRef;
      function TString(schema) {
        return TKindOf(schema, "String") && schema.type === "string" && IsOptionalString(schema.$id) && IsOptionalNumber(schema.minLength) && IsOptionalNumber(schema.maxLength) && IsOptionalPattern(schema.pattern) && IsOptionalFormat(schema.format);
      }
      TypeGuard2.TString = TString;
      function TSymbol(schema) {
        return TKindOf(schema, "Symbol") && schema.type === "symbol" && IsOptionalString(schema.$id);
      }
      TypeGuard2.TSymbol = TSymbol;
      function TTemplateLiteral(schema) {
        return TKindOf(schema, "TemplateLiteral") && schema.type === "string" && ValueGuard.IsString(schema.pattern) && schema.pattern[0] === "^" && schema.pattern[schema.pattern.length - 1] === "$";
      }
      TypeGuard2.TTemplateLiteral = TTemplateLiteral;
      function TThis(schema) {
        return TKindOf(schema, "This") && IsOptionalString(schema.$id) && ValueGuard.IsString(schema.$ref);
      }
      TypeGuard2.TThis = TThis;
      function TTransform(schema) {
        return ValueGuard.IsObject(schema) && exports.Transform in schema;
      }
      TypeGuard2.TTransform = TTransform;
      function TTuple(schema) {
        return TKindOf(schema, "Tuple") && schema.type === "array" && IsOptionalString(schema.$id) && ValueGuard.IsNumber(schema.minItems) && ValueGuard.IsNumber(schema.maxItems) && schema.minItems === schema.maxItems && // empty
        (ValueGuard.IsUndefined(schema.items) && ValueGuard.IsUndefined(schema.additionalItems) && schema.minItems === 0 || ValueGuard.IsArray(schema.items) && schema.items.every((schema2) => TSchema(schema2)));
      }
      TypeGuard2.TTuple = TTuple;
      function TUndefined(schema) {
        return TKindOf(schema, "Undefined") && schema.type === "undefined" && IsOptionalString(schema.$id);
      }
      TypeGuard2.TUndefined = TUndefined;
      function TUnionLiteral(schema) {
        return TUnion(schema) && schema.anyOf.every((schema2) => TLiteralString(schema2) || TLiteralNumber(schema2));
      }
      TypeGuard2.TUnionLiteral = TUnionLiteral;
      function TUnion(schema) {
        return TKindOf(schema, "Union") && IsOptionalString(schema.$id) && ValueGuard.IsObject(schema) && ValueGuard.IsArray(schema.anyOf) && schema.anyOf.every((schema2) => TSchema(schema2));
      }
      TypeGuard2.TUnion = TUnion;
      function TUint8Array(schema) {
        return TKindOf(schema, "Uint8Array") && schema.type === "Uint8Array" && IsOptionalString(schema.$id) && IsOptionalNumber(schema.minByteLength) && IsOptionalNumber(schema.maxByteLength);
      }
      TypeGuard2.TUint8Array = TUint8Array;
      function TUnknown(schema) {
        return TKindOf(schema, "Unknown") && IsOptionalString(schema.$id);
      }
      TypeGuard2.TUnknown = TUnknown;
      function TUnsafe(schema) {
        return TKindOf(schema, "Unsafe");
      }
      TypeGuard2.TUnsafe = TUnsafe;
      function TVoid(schema) {
        return TKindOf(schema, "Void") && schema.type === "void" && IsOptionalString(schema.$id);
      }
      TypeGuard2.TVoid = TVoid;
      function TReadonly(schema) {
        return ValueGuard.IsObject(schema) && schema[exports.Readonly] === "Readonly";
      }
      TypeGuard2.TReadonly = TReadonly;
      function TOptional(schema) {
        return ValueGuard.IsObject(schema) && schema[exports.Optional] === "Optional";
      }
      TypeGuard2.TOptional = TOptional;
      function TSchema(schema) {
        return ValueGuard.IsObject(schema) && (TAny(schema) || TArray(schema) || TBoolean(schema) || TBigInt(schema) || TAsyncIterator(schema) || TConstructor(schema) || TDate(schema) || TFunction(schema) || TInteger(schema) || TIntersect(schema) || TIterator(schema) || TLiteral(schema) || TNever(schema) || TNot(schema) || TNull(schema) || TNumber(schema) || TObject(schema) || TPromise(schema) || TRecord(schema) || TRef(schema) || TString(schema) || TSymbol(schema) || TTemplateLiteral(schema) || TThis(schema) || TTuple(schema) || TUndefined(schema) || TUnion(schema) || TUint8Array(schema) || TUnknown(schema) || TUnsafe(schema) || TVoid(schema) || TKind(schema) && TypeRegistry.Has(schema[exports.Kind]));
      }
      TypeGuard2.TSchema = TSchema;
    })(TypeGuard || (exports.TypeGuard = TypeGuard = {}));
    var ExtendsUndefined;
    (function(ExtendsUndefined2) {
      function Check(schema) {
        return schema[exports.Kind] === "Intersect" ? schema.allOf.every((schema2) => Check(schema2)) : schema[exports.Kind] === "Union" ? schema.anyOf.some((schema2) => Check(schema2)) : schema[exports.Kind] === "Undefined" ? true : schema[exports.Kind] === "Not" ? !Check(schema.not) : false;
      }
      ExtendsUndefined2.Check = Check;
    })(ExtendsUndefined || (exports.ExtendsUndefined = ExtendsUndefined = {}));
    var TypeExtendsError = class extends TypeBoxError {
    };
    exports.TypeExtendsError = TypeExtendsError;
    var TypeExtendsResult;
    (function(TypeExtendsResult2) {
      TypeExtendsResult2[TypeExtendsResult2["Union"] = 0] = "Union";
      TypeExtendsResult2[TypeExtendsResult2["True"] = 1] = "True";
      TypeExtendsResult2[TypeExtendsResult2["False"] = 2] = "False";
    })(TypeExtendsResult || (exports.TypeExtendsResult = TypeExtendsResult = {}));
    var TypeExtends;
    (function(TypeExtends2) {
      function IntoBooleanResult(result) {
        return result === TypeExtendsResult.False ? result : TypeExtendsResult.True;
      }
      function Throw(message) {
        throw new TypeExtendsError(message);
      }
      function IsStructuralRight(right) {
        return TypeGuard.TNever(right) || TypeGuard.TIntersect(right) || TypeGuard.TUnion(right) || TypeGuard.TUnknown(right) || TypeGuard.TAny(right);
      }
      function StructuralRight(left, right) {
        return TypeGuard.TNever(right) ? TNeverRight(left, right) : TypeGuard.TIntersect(right) ? TIntersectRight(left, right) : TypeGuard.TUnion(right) ? TUnionRight(left, right) : TypeGuard.TUnknown(right) ? TUnknownRight(left, right) : TypeGuard.TAny(right) ? TAnyRight(left, right) : Throw("StructuralRight");
      }
      function TAnyRight(left, right) {
        return TypeExtendsResult.True;
      }
      function TAny(left, right) {
        return TypeGuard.TIntersect(right) ? TIntersectRight(left, right) : TypeGuard.TUnion(right) && right.anyOf.some((schema) => TypeGuard.TAny(schema) || TypeGuard.TUnknown(schema)) ? TypeExtendsResult.True : TypeGuard.TUnion(right) ? TypeExtendsResult.Union : TypeGuard.TUnknown(right) ? TypeExtendsResult.True : TypeGuard.TAny(right) ? TypeExtendsResult.True : TypeExtendsResult.Union;
      }
      function TArrayRight(left, right) {
        return TypeGuard.TUnknown(left) ? TypeExtendsResult.False : TypeGuard.TAny(left) ? TypeExtendsResult.Union : TypeGuard.TNever(left) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TArray(left, right) {
        return TypeGuard.TObject(right) && IsObjectArrayLike(right) ? TypeExtendsResult.True : IsStructuralRight(right) ? StructuralRight(left, right) : !TypeGuard.TArray(right) ? TypeExtendsResult.False : IntoBooleanResult(Visit(left.items, right.items));
      }
      function TAsyncIterator(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : !TypeGuard.TAsyncIterator(right) ? TypeExtendsResult.False : IntoBooleanResult(Visit(left.items, right.items));
      }
      function TBigInt(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TBigInt(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TBooleanRight(left, right) {
        return TypeGuard.TLiteral(left) && ValueGuard.IsBoolean(left.const) ? TypeExtendsResult.True : TypeGuard.TBoolean(left) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TBoolean(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TBoolean(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TConstructor(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : !TypeGuard.TConstructor(right) ? TypeExtendsResult.False : left.parameters.length > right.parameters.length ? TypeExtendsResult.False : !left.parameters.every((schema, index) => IntoBooleanResult(Visit(right.parameters[index], schema)) === TypeExtendsResult.True) ? TypeExtendsResult.False : IntoBooleanResult(Visit(left.returns, right.returns));
      }
      function TDate(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TDate(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TFunction(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : !TypeGuard.TFunction(right) ? TypeExtendsResult.False : left.parameters.length > right.parameters.length ? TypeExtendsResult.False : !left.parameters.every((schema, index) => IntoBooleanResult(Visit(right.parameters[index], schema)) === TypeExtendsResult.True) ? TypeExtendsResult.False : IntoBooleanResult(Visit(left.returns, right.returns));
      }
      function TIntegerRight(left, right) {
        return TypeGuard.TLiteral(left) && ValueGuard.IsNumber(left.const) ? TypeExtendsResult.True : TypeGuard.TNumber(left) || TypeGuard.TInteger(left) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TInteger(left, right) {
        return TypeGuard.TInteger(right) || TypeGuard.TNumber(right) ? TypeExtendsResult.True : IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeExtendsResult.False;
      }
      function TIntersectRight(left, right) {
        return right.allOf.every((schema) => Visit(left, schema) === TypeExtendsResult.True) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TIntersect(left, right) {
        return left.allOf.some((schema) => Visit(schema, right) === TypeExtendsResult.True) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TIterator(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : !TypeGuard.TIterator(right) ? TypeExtendsResult.False : IntoBooleanResult(Visit(left.items, right.items));
      }
      function TLiteral(left, right) {
        return TypeGuard.TLiteral(right) && right.const === left.const ? TypeExtendsResult.True : IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TString(right) ? TStringRight(left, right) : TypeGuard.TNumber(right) ? TNumberRight(left, right) : TypeGuard.TInteger(right) ? TIntegerRight(left, right) : TypeGuard.TBoolean(right) ? TBooleanRight(left, right) : TypeExtendsResult.False;
      }
      function TNeverRight(left, right) {
        return TypeExtendsResult.False;
      }
      function TNever(left, right) {
        return TypeExtendsResult.True;
      }
      function UnwrapTNot(schema) {
        let [current, depth] = [schema, 0];
        while (true) {
          if (!TypeGuard.TNot(current))
            break;
          current = current.not;
          depth += 1;
        }
        return depth % 2 === 0 ? current : exports.Type.Unknown();
      }
      function TNot(left, right) {
        return TypeGuard.TNot(left) ? Visit(UnwrapTNot(left), right) : TypeGuard.TNot(right) ? Visit(left, UnwrapTNot(right)) : Throw("Invalid fallthrough for Not");
      }
      function TNull(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TNull(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TNumberRight(left, right) {
        return TypeGuard.TLiteralNumber(left) ? TypeExtendsResult.True : TypeGuard.TNumber(left) || TypeGuard.TInteger(left) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TNumber(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TInteger(right) || TypeGuard.TNumber(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function IsObjectPropertyCount(schema, count) {
        return Object.getOwnPropertyNames(schema.properties).length === count;
      }
      function IsObjectStringLike(schema) {
        return IsObjectArrayLike(schema);
      }
      function IsObjectSymbolLike(schema) {
        return IsObjectPropertyCount(schema, 0) || IsObjectPropertyCount(schema, 1) && "description" in schema.properties && TypeGuard.TUnion(schema.properties.description) && schema.properties.description.anyOf.length === 2 && (TypeGuard.TString(schema.properties.description.anyOf[0]) && TypeGuard.TUndefined(schema.properties.description.anyOf[1]) || TypeGuard.TString(schema.properties.description.anyOf[1]) && TypeGuard.TUndefined(schema.properties.description.anyOf[0]));
      }
      function IsObjectNumberLike(schema) {
        return IsObjectPropertyCount(schema, 0);
      }
      function IsObjectBooleanLike(schema) {
        return IsObjectPropertyCount(schema, 0);
      }
      function IsObjectBigIntLike(schema) {
        return IsObjectPropertyCount(schema, 0);
      }
      function IsObjectDateLike(schema) {
        return IsObjectPropertyCount(schema, 0);
      }
      function IsObjectUint8ArrayLike(schema) {
        return IsObjectArrayLike(schema);
      }
      function IsObjectFunctionLike(schema) {
        const length = exports.Type.Number();
        return IsObjectPropertyCount(schema, 0) || IsObjectPropertyCount(schema, 1) && "length" in schema.properties && IntoBooleanResult(Visit(schema.properties["length"], length)) === TypeExtendsResult.True;
      }
      function IsObjectConstructorLike(schema) {
        return IsObjectPropertyCount(schema, 0);
      }
      function IsObjectArrayLike(schema) {
        const length = exports.Type.Number();
        return IsObjectPropertyCount(schema, 0) || IsObjectPropertyCount(schema, 1) && "length" in schema.properties && IntoBooleanResult(Visit(schema.properties["length"], length)) === TypeExtendsResult.True;
      }
      function IsObjectPromiseLike(schema) {
        const then = exports.Type.Function([exports.Type.Any()], exports.Type.Any());
        return IsObjectPropertyCount(schema, 0) || IsObjectPropertyCount(schema, 1) && "then" in schema.properties && IntoBooleanResult(Visit(schema.properties["then"], then)) === TypeExtendsResult.True;
      }
      function Property(left, right) {
        return Visit(left, right) === TypeExtendsResult.False ? TypeExtendsResult.False : TypeGuard.TOptional(left) && !TypeGuard.TOptional(right) ? TypeExtendsResult.False : TypeExtendsResult.True;
      }
      function TObjectRight(left, right) {
        return TypeGuard.TUnknown(left) ? TypeExtendsResult.False : TypeGuard.TAny(left) ? TypeExtendsResult.Union : TypeGuard.TNever(left) || TypeGuard.TLiteralString(left) && IsObjectStringLike(right) || TypeGuard.TLiteralNumber(left) && IsObjectNumberLike(right) || TypeGuard.TLiteralBoolean(left) && IsObjectBooleanLike(right) || TypeGuard.TSymbol(left) && IsObjectSymbolLike(right) || TypeGuard.TBigInt(left) && IsObjectBigIntLike(right) || TypeGuard.TString(left) && IsObjectStringLike(right) || TypeGuard.TSymbol(left) && IsObjectSymbolLike(right) || TypeGuard.TNumber(left) && IsObjectNumberLike(right) || TypeGuard.TInteger(left) && IsObjectNumberLike(right) || TypeGuard.TBoolean(left) && IsObjectBooleanLike(right) || TypeGuard.TUint8Array(left) && IsObjectUint8ArrayLike(right) || TypeGuard.TDate(left) && IsObjectDateLike(right) || TypeGuard.TConstructor(left) && IsObjectConstructorLike(right) || TypeGuard.TFunction(left) && IsObjectFunctionLike(right) ? TypeExtendsResult.True : TypeGuard.TRecord(left) && TypeGuard.TString(RecordKey(left)) ? (() => {
          return right[exports.Hint] === "Record" ? TypeExtendsResult.True : TypeExtendsResult.False;
        })() : TypeGuard.TRecord(left) && TypeGuard.TNumber(RecordKey(left)) ? (() => {
          return IsObjectPropertyCount(right, 0) ? TypeExtendsResult.True : TypeExtendsResult.False;
        })() : TypeExtendsResult.False;
      }
      function TObject(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : !TypeGuard.TObject(right) ? TypeExtendsResult.False : (() => {
          for (const key of Object.getOwnPropertyNames(right.properties)) {
            if (!(key in left.properties) && !TypeGuard.TOptional(right.properties[key])) {
              return TypeExtendsResult.False;
            }
            if (TypeGuard.TOptional(right.properties[key])) {
              return TypeExtendsResult.True;
            }
            if (Property(left.properties[key], right.properties[key]) === TypeExtendsResult.False) {
              return TypeExtendsResult.False;
            }
          }
          return TypeExtendsResult.True;
        })();
      }
      function TPromise(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) && IsObjectPromiseLike(right) ? TypeExtendsResult.True : !TypeGuard.TPromise(right) ? TypeExtendsResult.False : IntoBooleanResult(Visit(left.item, right.item));
      }
      function RecordKey(schema) {
        return exports.PatternNumberExact in schema.patternProperties ? exports.Type.Number() : exports.PatternStringExact in schema.patternProperties ? exports.Type.String() : Throw("Unknown record key pattern");
      }
      function RecordValue(schema) {
        return exports.PatternNumberExact in schema.patternProperties ? schema.patternProperties[exports.PatternNumberExact] : exports.PatternStringExact in schema.patternProperties ? schema.patternProperties[exports.PatternStringExact] : Throw("Unable to get record value schema");
      }
      function TRecordRight(left, right) {
        const [Key, Value] = [RecordKey(right), RecordValue(right)];
        return TypeGuard.TLiteralString(left) && TypeGuard.TNumber(Key) && IntoBooleanResult(Visit(left, Value)) === TypeExtendsResult.True ? TypeExtendsResult.True : TypeGuard.TUint8Array(left) && TypeGuard.TNumber(Key) ? Visit(left, Value) : TypeGuard.TString(left) && TypeGuard.TNumber(Key) ? Visit(left, Value) : TypeGuard.TArray(left) && TypeGuard.TNumber(Key) ? Visit(left, Value) : TypeGuard.TObject(left) ? (() => {
          for (const key of Object.getOwnPropertyNames(left.properties)) {
            if (Property(Value, left.properties[key]) === TypeExtendsResult.False) {
              return TypeExtendsResult.False;
            }
          }
          return TypeExtendsResult.True;
        })() : TypeExtendsResult.False;
      }
      function TRecord(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : !TypeGuard.TRecord(right) ? TypeExtendsResult.False : Visit(RecordValue(left), RecordValue(right));
      }
      function TStringRight(left, right) {
        return TypeGuard.TLiteral(left) && ValueGuard.IsString(left.const) ? TypeExtendsResult.True : TypeGuard.TString(left) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TString(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TString(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TSymbol(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TSymbol(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TTemplateLiteral(left, right) {
        return TypeGuard.TTemplateLiteral(left) ? Visit(TemplateLiteralResolver.Resolve(left), right) : TypeGuard.TTemplateLiteral(right) ? Visit(left, TemplateLiteralResolver.Resolve(right)) : Throw("Invalid fallthrough for TemplateLiteral");
      }
      function IsArrayOfTuple(left, right) {
        return TypeGuard.TArray(right) && left.items !== void 0 && left.items.every((schema) => Visit(schema, right.items) === TypeExtendsResult.True);
      }
      function TTupleRight(left, right) {
        return TypeGuard.TNever(left) ? TypeExtendsResult.True : TypeGuard.TUnknown(left) ? TypeExtendsResult.False : TypeGuard.TAny(left) ? TypeExtendsResult.Union : TypeExtendsResult.False;
      }
      function TTuple(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) && IsObjectArrayLike(right) ? TypeExtendsResult.True : TypeGuard.TArray(right) && IsArrayOfTuple(left, right) ? TypeExtendsResult.True : !TypeGuard.TTuple(right) ? TypeExtendsResult.False : ValueGuard.IsUndefined(left.items) && !ValueGuard.IsUndefined(right.items) || !ValueGuard.IsUndefined(left.items) && ValueGuard.IsUndefined(right.items) ? TypeExtendsResult.False : ValueGuard.IsUndefined(left.items) && !ValueGuard.IsUndefined(right.items) ? TypeExtendsResult.True : left.items.every((schema, index) => Visit(schema, right.items[index]) === TypeExtendsResult.True) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TUint8Array(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TUint8Array(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TUndefined(left, right) {
        return IsStructuralRight(right) ? StructuralRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TRecord(right) ? TRecordRight(left, right) : TypeGuard.TVoid(right) ? VoidRight(left, right) : TypeGuard.TUndefined(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TUnionRight(left, right) {
        return right.anyOf.some((schema) => Visit(left, schema) === TypeExtendsResult.True) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TUnion(left, right) {
        return left.anyOf.every((schema) => Visit(schema, right) === TypeExtendsResult.True) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TUnknownRight(left, right) {
        return TypeExtendsResult.True;
      }
      function TUnknown(left, right) {
        return TypeGuard.TNever(right) ? TNeverRight(left, right) : TypeGuard.TIntersect(right) ? TIntersectRight(left, right) : TypeGuard.TUnion(right) ? TUnionRight(left, right) : TypeGuard.TAny(right) ? TAnyRight(left, right) : TypeGuard.TString(right) ? TStringRight(left, right) : TypeGuard.TNumber(right) ? TNumberRight(left, right) : TypeGuard.TInteger(right) ? TIntegerRight(left, right) : TypeGuard.TBoolean(right) ? TBooleanRight(left, right) : TypeGuard.TArray(right) ? TArrayRight(left, right) : TypeGuard.TTuple(right) ? TTupleRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TUnknown(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function VoidRight(left, right) {
        return TypeGuard.TUndefined(left) ? TypeExtendsResult.True : TypeGuard.TUndefined(left) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function TVoid(left, right) {
        return TypeGuard.TIntersect(right) ? TIntersectRight(left, right) : TypeGuard.TUnion(right) ? TUnionRight(left, right) : TypeGuard.TUnknown(right) ? TUnknownRight(left, right) : TypeGuard.TAny(right) ? TAnyRight(left, right) : TypeGuard.TObject(right) ? TObjectRight(left, right) : TypeGuard.TVoid(right) ? TypeExtendsResult.True : TypeExtendsResult.False;
      }
      function Visit(left, right) {
        return (
          // resolvable
          TypeGuard.TTemplateLiteral(left) || TypeGuard.TTemplateLiteral(right) ? TTemplateLiteral(left, right) : TypeGuard.TNot(left) || TypeGuard.TNot(right) ? TNot(left, right) : (
            // standard
            TypeGuard.TAny(left) ? TAny(left, right) : TypeGuard.TArray(left) ? TArray(left, right) : TypeGuard.TBigInt(left) ? TBigInt(left, right) : TypeGuard.TBoolean(left) ? TBoolean(left, right) : TypeGuard.TAsyncIterator(left) ? TAsyncIterator(left, right) : TypeGuard.TConstructor(left) ? TConstructor(left, right) : TypeGuard.TDate(left) ? TDate(left, right) : TypeGuard.TFunction(left) ? TFunction(left, right) : TypeGuard.TInteger(left) ? TInteger(left, right) : TypeGuard.TIntersect(left) ? TIntersect(left, right) : TypeGuard.TIterator(left) ? TIterator(left, right) : TypeGuard.TLiteral(left) ? TLiteral(left, right) : TypeGuard.TNever(left) ? TNever(left, right) : TypeGuard.TNull(left) ? TNull(left, right) : TypeGuard.TNumber(left) ? TNumber(left, right) : TypeGuard.TObject(left) ? TObject(left, right) : TypeGuard.TRecord(left) ? TRecord(left, right) : TypeGuard.TString(left) ? TString(left, right) : TypeGuard.TSymbol(left) ? TSymbol(left, right) : TypeGuard.TTuple(left) ? TTuple(left, right) : TypeGuard.TPromise(left) ? TPromise(left, right) : TypeGuard.TUint8Array(left) ? TUint8Array(left, right) : TypeGuard.TUndefined(left) ? TUndefined(left, right) : TypeGuard.TUnion(left) ? TUnion(left, right) : TypeGuard.TUnknown(left) ? TUnknown(left, right) : TypeGuard.TVoid(left) ? TVoid(left, right) : Throw(`Unknown left type operand '${left[exports.Kind]}'`)
          )
        );
      }
      function Extends(left, right) {
        return Visit(left, right);
      }
      TypeExtends2.Extends = Extends;
    })(TypeExtends || (exports.TypeExtends = TypeExtends = {}));
    var TypeClone;
    (function(TypeClone2) {
      function ArrayType(value) {
        return value.map((value2) => Visit(value2));
      }
      function DateType(value) {
        return new Date(value.getTime());
      }
      function Uint8ArrayType(value) {
        return new Uint8Array(value);
      }
      function ObjectType(value) {
        const clonedProperties = Object.getOwnPropertyNames(value).reduce((acc, key) => ({ ...acc, [key]: Visit(value[key]) }), {});
        const clonedSymbols = Object.getOwnPropertySymbols(value).reduce((acc, key) => ({ ...acc, [key]: Visit(value[key]) }), {});
        return { ...clonedProperties, ...clonedSymbols };
      }
      function Visit(value) {
        return ValueGuard.IsArray(value) ? ArrayType(value) : ValueGuard.IsDate(value) ? DateType(value) : ValueGuard.IsUint8Array(value) ? Uint8ArrayType(value) : ValueGuard.IsObject(value) ? ObjectType(value) : value;
      }
      function Rest(schemas) {
        return schemas.map((schema) => Type2(schema));
      }
      TypeClone2.Rest = Rest;
      function Type2(schema, options = {}) {
        return { ...Visit(schema), ...options };
      }
      TypeClone2.Type = Type2;
    })(TypeClone || (exports.TypeClone = TypeClone = {}));
    var IndexedAccessor;
    (function(IndexedAccessor2) {
      function OptionalUnwrap(schema) {
        return schema.map((schema2) => {
          const { [exports.Optional]: _, ...clone } = TypeClone.Type(schema2);
          return clone;
        });
      }
      function IsIntersectOptional(schema) {
        return schema.every((schema2) => TypeGuard.TOptional(schema2));
      }
      function IsUnionOptional(schema) {
        return schema.some((schema2) => TypeGuard.TOptional(schema2));
      }
      function ResolveIntersect(schema) {
        return IsIntersectOptional(schema.allOf) ? exports.Type.Optional(exports.Type.Intersect(OptionalUnwrap(schema.allOf))) : schema;
      }
      function ResolveUnion(schema) {
        return IsUnionOptional(schema.anyOf) ? exports.Type.Optional(exports.Type.Union(OptionalUnwrap(schema.anyOf))) : schema;
      }
      function ResolveOptional(schema) {
        return schema[exports.Kind] === "Intersect" ? ResolveIntersect(schema) : schema[exports.Kind] === "Union" ? ResolveUnion(schema) : schema;
      }
      function TIntersect(schema, key) {
        const resolved = schema.allOf.reduce((acc, schema2) => {
          const indexed = Visit(schema2, key);
          return indexed[exports.Kind] === "Never" ? acc : [...acc, indexed];
        }, []);
        return ResolveOptional(exports.Type.Intersect(resolved));
      }
      function TUnion(schema, key) {
        const resolved = schema.anyOf.map((schema2) => Visit(schema2, key));
        return ResolveOptional(exports.Type.Union(resolved));
      }
      function TObject(schema, key) {
        const property = schema.properties[key];
        return ValueGuard.IsUndefined(property) ? exports.Type.Never() : exports.Type.Union([property]);
      }
      function TTuple(schema, key) {
        const items = schema.items;
        if (ValueGuard.IsUndefined(items))
          return exports.Type.Never();
        const element = items[key];
        if (ValueGuard.IsUndefined(element))
          return exports.Type.Never();
        return element;
      }
      function Visit(schema, key) {
        return schema[exports.Kind] === "Intersect" ? TIntersect(schema, key) : schema[exports.Kind] === "Union" ? TUnion(schema, key) : schema[exports.Kind] === "Object" ? TObject(schema, key) : schema[exports.Kind] === "Tuple" ? TTuple(schema, key) : exports.Type.Never();
      }
      function Resolve(schema, keys, options = {}) {
        const resolved = keys.map((key) => Visit(schema, key.toString()));
        return ResolveOptional(exports.Type.Union(resolved, options));
      }
      IndexedAccessor2.Resolve = Resolve;
    })(IndexedAccessor || (exports.IndexedAccessor = IndexedAccessor = {}));
    var Intrinsic;
    (function(Intrinsic2) {
      function Uncapitalize(value) {
        const [first, rest] = [value.slice(0, 1), value.slice(1)];
        return `${first.toLowerCase()}${rest}`;
      }
      function Capitalize(value) {
        const [first, rest] = [value.slice(0, 1), value.slice(1)];
        return `${first.toUpperCase()}${rest}`;
      }
      function Uppercase(value) {
        return value.toUpperCase();
      }
      function Lowercase(value) {
        return value.toLowerCase();
      }
      function IntrinsicTemplateLiteral(schema, mode) {
        const expression = TemplateLiteralParser.ParseExact(schema.pattern);
        const finite = TemplateLiteralFinite.Check(expression);
        if (!finite)
          return { ...schema, pattern: IntrinsicLiteral(schema.pattern, mode) };
        const strings = [...TemplateLiteralGenerator.Generate(expression)];
        const literals = strings.map((value) => exports.Type.Literal(value));
        const mapped = IntrinsicRest(literals, mode);
        const union = exports.Type.Union(mapped);
        return exports.Type.TemplateLiteral([union]);
      }
      function IntrinsicLiteral(value, mode) {
        return typeof value === "string" ? mode === "Uncapitalize" ? Uncapitalize(value) : mode === "Capitalize" ? Capitalize(value) : mode === "Uppercase" ? Uppercase(value) : mode === "Lowercase" ? Lowercase(value) : value : value.toString();
      }
      function IntrinsicRest(schema, mode) {
        if (schema.length === 0)
          return [];
        const [L, ...R] = schema;
        return [Map2(L, mode), ...IntrinsicRest(R, mode)];
      }
      function Visit(schema, mode) {
        return TypeGuard.TTemplateLiteral(schema) ? IntrinsicTemplateLiteral(schema, mode) : TypeGuard.TUnion(schema) ? exports.Type.Union(IntrinsicRest(schema.anyOf, mode)) : TypeGuard.TLiteral(schema) ? exports.Type.Literal(IntrinsicLiteral(schema.const, mode)) : schema;
      }
      function Map2(schema, mode) {
        return Visit(schema, mode);
      }
      Intrinsic2.Map = Map2;
    })(Intrinsic || (exports.Intrinsic = Intrinsic = {}));
    var ObjectMap;
    (function(ObjectMap2) {
      function TIntersect(schema, callback) {
        return exports.Type.Intersect(schema.allOf.map((inner) => Visit(inner, callback)), { ...schema });
      }
      function TUnion(schema, callback) {
        return exports.Type.Union(schema.anyOf.map((inner) => Visit(inner, callback)), { ...schema });
      }
      function TObject(schema, callback) {
        return callback(schema);
      }
      function Visit(schema, callback) {
        return schema[exports.Kind] === "Intersect" ? TIntersect(schema, callback) : schema[exports.Kind] === "Union" ? TUnion(schema, callback) : schema[exports.Kind] === "Object" ? TObject(schema, callback) : schema;
      }
      function Map2(schema, callback, options) {
        return { ...Visit(TypeClone.Type(schema), callback), ...options };
      }
      ObjectMap2.Map = Map2;
    })(ObjectMap || (exports.ObjectMap = ObjectMap = {}));
    var KeyResolver;
    (function(KeyResolver2) {
      function UnwrapPattern(key) {
        return key[0] === "^" && key[key.length - 1] === "$" ? key.slice(1, key.length - 1) : key;
      }
      function TIntersect(schema, options) {
        return schema.allOf.reduce((acc, schema2) => [...acc, ...Visit(schema2, options)], []);
      }
      function TUnion(schema, options) {
        const sets = schema.anyOf.map((inner) => Visit(inner, options));
        return [...sets.reduce((set, outer) => outer.map((key) => sets.every((inner) => inner.includes(key)) ? set.add(key) : set)[0], /* @__PURE__ */ new Set())];
      }
      function TObject(schema, options) {
        return Object.getOwnPropertyNames(schema.properties);
      }
      function TRecord(schema, options) {
        return options.includePatterns ? Object.getOwnPropertyNames(schema.patternProperties) : [];
      }
      function Visit(schema, options) {
        return TypeGuard.TIntersect(schema) ? TIntersect(schema, options) : TypeGuard.TUnion(schema) ? TUnion(schema, options) : TypeGuard.TObject(schema) ? TObject(schema, options) : TypeGuard.TRecord(schema) ? TRecord(schema, options) : [];
      }
      function ResolveKeys(schema, options) {
        return [...new Set(Visit(schema, options))];
      }
      KeyResolver2.ResolveKeys = ResolveKeys;
      function ResolvePattern(schema) {
        const keys = ResolveKeys(schema, { includePatterns: true });
        const pattern = keys.map((key) => `(${UnwrapPattern(key)})`);
        return `^(${pattern.join("|")})$`;
      }
      KeyResolver2.ResolvePattern = ResolvePattern;
    })(KeyResolver || (exports.KeyResolver = KeyResolver = {}));
    var KeyArrayResolverError = class extends TypeBoxError {
    };
    exports.KeyArrayResolverError = KeyArrayResolverError;
    var KeyArrayResolver;
    (function(KeyArrayResolver2) {
      function Resolve(schema) {
        return Array.isArray(schema) ? schema : TypeGuard.TUnionLiteral(schema) ? schema.anyOf.map((schema2) => schema2.const.toString()) : TypeGuard.TLiteral(schema) ? [schema.const] : TypeGuard.TTemplateLiteral(schema) ? (() => {
          const expression = TemplateLiteralParser.ParseExact(schema.pattern);
          if (!TemplateLiteralFinite.Check(expression))
            throw new KeyArrayResolverError("Cannot resolve keys from infinite template expression");
          return [...TemplateLiteralGenerator.Generate(expression)];
        })() : [];
      }
      KeyArrayResolver2.Resolve = Resolve;
    })(KeyArrayResolver || (exports.KeyArrayResolver = KeyArrayResolver = {}));
    var UnionResolver;
    (function(UnionResolver2) {
      function* TUnion(union) {
        for (const schema of union.anyOf) {
          if (schema[exports.Kind] === "Union") {
            yield* TUnion(schema);
          } else {
            yield schema;
          }
        }
      }
      function Resolve(union) {
        return exports.Type.Union([...TUnion(union)], { ...union });
      }
      UnionResolver2.Resolve = Resolve;
    })(UnionResolver || (exports.UnionResolver = UnionResolver = {}));
    var TemplateLiteralPatternError = class extends TypeBoxError {
    };
    exports.TemplateLiteralPatternError = TemplateLiteralPatternError;
    var TemplateLiteralPattern;
    (function(TemplateLiteralPattern2) {
      function Throw(message) {
        throw new TemplateLiteralPatternError(message);
      }
      function Escape(value) {
        return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      }
      function Visit(schema, acc) {
        return TypeGuard.TTemplateLiteral(schema) ? schema.pattern.slice(1, schema.pattern.length - 1) : TypeGuard.TUnion(schema) ? `(${schema.anyOf.map((schema2) => Visit(schema2, acc)).join("|")})` : TypeGuard.TNumber(schema) ? `${acc}${exports.PatternNumber}` : TypeGuard.TInteger(schema) ? `${acc}${exports.PatternNumber}` : TypeGuard.TBigInt(schema) ? `${acc}${exports.PatternNumber}` : TypeGuard.TString(schema) ? `${acc}${exports.PatternString}` : TypeGuard.TLiteral(schema) ? `${acc}${Escape(schema.const.toString())}` : TypeGuard.TBoolean(schema) ? `${acc}${exports.PatternBoolean}` : Throw(`Unexpected Kind '${schema[exports.Kind]}'`);
      }
      function Create(kinds) {
        return `^${kinds.map((schema) => Visit(schema, "")).join("")}$`;
      }
      TemplateLiteralPattern2.Create = Create;
    })(TemplateLiteralPattern || (exports.TemplateLiteralPattern = TemplateLiteralPattern = {}));
    var TemplateLiteralResolver;
    (function(TemplateLiteralResolver2) {
      function Resolve(template) {
        const expression = TemplateLiteralParser.ParseExact(template.pattern);
        if (!TemplateLiteralFinite.Check(expression))
          return exports.Type.String();
        const literals = [...TemplateLiteralGenerator.Generate(expression)].map((value) => exports.Type.Literal(value));
        return exports.Type.Union(literals);
      }
      TemplateLiteralResolver2.Resolve = Resolve;
    })(TemplateLiteralResolver || (exports.TemplateLiteralResolver = TemplateLiteralResolver = {}));
    var TemplateLiteralParserError = class extends TypeBoxError {
    };
    exports.TemplateLiteralParserError = TemplateLiteralParserError;
    var TemplateLiteralParser;
    (function(TemplateLiteralParser2) {
      function IsNonEscaped(pattern, index, char) {
        return pattern[index] === char && pattern.charCodeAt(index - 1) !== 92;
      }
      function IsOpenParen(pattern, index) {
        return IsNonEscaped(pattern, index, "(");
      }
      function IsCloseParen(pattern, index) {
        return IsNonEscaped(pattern, index, ")");
      }
      function IsSeparator(pattern, index) {
        return IsNonEscaped(pattern, index, "|");
      }
      function IsGroup(pattern) {
        if (!(IsOpenParen(pattern, 0) && IsCloseParen(pattern, pattern.length - 1)))
          return false;
        let count = 0;
        for (let index = 0; index < pattern.length; index++) {
          if (IsOpenParen(pattern, index))
            count += 1;
          if (IsCloseParen(pattern, index))
            count -= 1;
          if (count === 0 && index !== pattern.length - 1)
            return false;
        }
        return true;
      }
      function InGroup(pattern) {
        return pattern.slice(1, pattern.length - 1);
      }
      function IsPrecedenceOr(pattern) {
        let count = 0;
        for (let index = 0; index < pattern.length; index++) {
          if (IsOpenParen(pattern, index))
            count += 1;
          if (IsCloseParen(pattern, index))
            count -= 1;
          if (IsSeparator(pattern, index) && count === 0)
            return true;
        }
        return false;
      }
      function IsPrecedenceAnd(pattern) {
        for (let index = 0; index < pattern.length; index++) {
          if (IsOpenParen(pattern, index))
            return true;
        }
        return false;
      }
      function Or(pattern) {
        let [count, start] = [0, 0];
        const expressions = [];
        for (let index = 0; index < pattern.length; index++) {
          if (IsOpenParen(pattern, index))
            count += 1;
          if (IsCloseParen(pattern, index))
            count -= 1;
          if (IsSeparator(pattern, index) && count === 0) {
            const range2 = pattern.slice(start, index);
            if (range2.length > 0)
              expressions.push(Parse(range2));
            start = index + 1;
          }
        }
        const range = pattern.slice(start);
        if (range.length > 0)
          expressions.push(Parse(range));
        if (expressions.length === 0)
          return { type: "const", const: "" };
        if (expressions.length === 1)
          return expressions[0];
        return { type: "or", expr: expressions };
      }
      function And(pattern) {
        function Group(value, index) {
          if (!IsOpenParen(value, index))
            throw new TemplateLiteralParserError(`TemplateLiteralParser: Index must point to open parens`);
          let count = 0;
          for (let scan = index; scan < value.length; scan++) {
            if (IsOpenParen(value, scan))
              count += 1;
            if (IsCloseParen(value, scan))
              count -= 1;
            if (count === 0)
              return [index, scan];
          }
          throw new TemplateLiteralParserError(`TemplateLiteralParser: Unclosed group parens in expression`);
        }
        function Range(pattern2, index) {
          for (let scan = index; scan < pattern2.length; scan++) {
            if (IsOpenParen(pattern2, scan))
              return [index, scan];
          }
          return [index, pattern2.length];
        }
        const expressions = [];
        for (let index = 0; index < pattern.length; index++) {
          if (IsOpenParen(pattern, index)) {
            const [start, end] = Group(pattern, index);
            const range = pattern.slice(start, end + 1);
            expressions.push(Parse(range));
            index = end;
          } else {
            const [start, end] = Range(pattern, index);
            const range = pattern.slice(start, end);
            if (range.length > 0)
              expressions.push(Parse(range));
            index = end - 1;
          }
        }
        return expressions.length === 0 ? { type: "const", const: "" } : expressions.length === 1 ? expressions[0] : { type: "and", expr: expressions };
      }
      function Parse(pattern) {
        return IsGroup(pattern) ? Parse(InGroup(pattern)) : IsPrecedenceOr(pattern) ? Or(pattern) : IsPrecedenceAnd(pattern) ? And(pattern) : { type: "const", const: pattern };
      }
      TemplateLiteralParser2.Parse = Parse;
      function ParseExact(pattern) {
        return Parse(pattern.slice(1, pattern.length - 1));
      }
      TemplateLiteralParser2.ParseExact = ParseExact;
    })(TemplateLiteralParser || (exports.TemplateLiteralParser = TemplateLiteralParser = {}));
    var TemplateLiteralFiniteError = class extends TypeBoxError {
    };
    exports.TemplateLiteralFiniteError = TemplateLiteralFiniteError;
    var TemplateLiteralFinite;
    (function(TemplateLiteralFinite2) {
      function Throw(message) {
        throw new TemplateLiteralFiniteError(message);
      }
      function IsNumber(expression) {
        return expression.type === "or" && expression.expr.length === 2 && expression.expr[0].type === "const" && expression.expr[0].const === "0" && expression.expr[1].type === "const" && expression.expr[1].const === "[1-9][0-9]*";
      }
      function IsBoolean(expression) {
        return expression.type === "or" && expression.expr.length === 2 && expression.expr[0].type === "const" && expression.expr[0].const === "true" && expression.expr[1].type === "const" && expression.expr[1].const === "false";
      }
      function IsString(expression) {
        return expression.type === "const" && expression.const === ".*";
      }
      function Check(expression) {
        return IsBoolean(expression) ? true : IsNumber(expression) || IsString(expression) ? false : expression.type === "and" ? expression.expr.every((expr) => Check(expr)) : expression.type === "or" ? expression.expr.every((expr) => Check(expr)) : expression.type === "const" ? true : Throw(`Unknown expression type`);
      }
      TemplateLiteralFinite2.Check = Check;
    })(TemplateLiteralFinite || (exports.TemplateLiteralFinite = TemplateLiteralFinite = {}));
    var TemplateLiteralGeneratorError = class extends TypeBoxError {
    };
    exports.TemplateLiteralGeneratorError = TemplateLiteralGeneratorError;
    var TemplateLiteralGenerator;
    (function(TemplateLiteralGenerator2) {
      function* Reduce(buffer) {
        if (buffer.length === 1)
          return yield* buffer[0];
        for (const left of buffer[0]) {
          for (const right of Reduce(buffer.slice(1))) {
            yield `${left}${right}`;
          }
        }
      }
      function* And(expression) {
        return yield* Reduce(expression.expr.map((expr) => [...Generate(expr)]));
      }
      function* Or(expression) {
        for (const expr of expression.expr)
          yield* Generate(expr);
      }
      function* Const(expression) {
        return yield expression.const;
      }
      function* Generate(expression) {
        return expression.type === "and" ? yield* And(expression) : expression.type === "or" ? yield* Or(expression) : expression.type === "const" ? yield* Const(expression) : (() => {
          throw new TemplateLiteralGeneratorError("Unknown expression");
        })();
      }
      TemplateLiteralGenerator2.Generate = Generate;
    })(TemplateLiteralGenerator || (exports.TemplateLiteralGenerator = TemplateLiteralGenerator = {}));
    var TemplateLiteralDslParser;
    (function(TemplateLiteralDslParser2) {
      function* ParseUnion(template) {
        const trim = template.trim().replace(/"|'/g, "");
        return trim === "boolean" ? yield exports.Type.Boolean() : trim === "number" ? yield exports.Type.Number() : trim === "bigint" ? yield exports.Type.BigInt() : trim === "string" ? yield exports.Type.String() : yield (() => {
          const literals = trim.split("|").map((literal) => exports.Type.Literal(literal.trim()));
          return literals.length === 0 ? exports.Type.Never() : literals.length === 1 ? literals[0] : exports.Type.Union(literals);
        })();
      }
      function* ParseTerminal(template) {
        if (template[1] !== "{") {
          const L = exports.Type.Literal("$");
          const R = ParseLiteral(template.slice(1));
          return yield* [L, ...R];
        }
        for (let i = 2; i < template.length; i++) {
          if (template[i] === "}") {
            const L = ParseUnion(template.slice(2, i));
            const R = ParseLiteral(template.slice(i + 1));
            return yield* [...L, ...R];
          }
        }
        yield exports.Type.Literal(template);
      }
      function* ParseLiteral(template) {
        for (let i = 0; i < template.length; i++) {
          if (template[i] === "$") {
            const L = exports.Type.Literal(template.slice(0, i));
            const R = ParseTerminal(template.slice(i));
            return yield* [L, ...R];
          }
        }
        yield exports.Type.Literal(template);
      }
      function Parse(template_dsl) {
        return [...ParseLiteral(template_dsl)];
      }
      TemplateLiteralDslParser2.Parse = Parse;
    })(TemplateLiteralDslParser || (exports.TemplateLiteralDslParser = TemplateLiteralDslParser = {}));
    var TransformDecodeBuilder = class {
      constructor(schema) {
        this.schema = schema;
      }
      Decode(decode) {
        return new TransformEncodeBuilder(this.schema, decode);
      }
    };
    exports.TransformDecodeBuilder = TransformDecodeBuilder;
    var TransformEncodeBuilder = class {
      constructor(schema, decode) {
        this.schema = schema;
        this.decode = decode;
      }
      Encode(encode) {
        const schema = TypeClone.Type(this.schema);
        return TypeGuard.TTransform(schema) ? (() => {
          const Encode = (value) => schema[exports.Transform].Encode(encode(value));
          const Decode = (value) => this.decode(schema[exports.Transform].Decode(value));
          const Codec = { Encode, Decode };
          return { ...schema, [exports.Transform]: Codec };
        })() : (() => {
          const Codec = { Decode: this.decode, Encode: encode };
          return { ...schema, [exports.Transform]: Codec };
        })();
      }
    };
    exports.TransformEncodeBuilder = TransformEncodeBuilder;
    var TypeOrdinal = 0;
    var TypeBuilderError = class extends TypeBoxError {
    };
    exports.TypeBuilderError = TypeBuilderError;
    var TypeBuilder = class {
      /** `[Internal]` Creates a schema without `static` and `params` types */
      Create(schema) {
        return schema;
      }
      /** `[Internal]` Throws a TypeBuilder error with the given message */
      Throw(message) {
        throw new TypeBuilderError(message);
      }
      /** `[Internal]` Discards property keys from the given record type */
      Discard(record, keys) {
        return keys.reduce((acc, key) => {
          const { [key]: _, ...rest } = acc;
          return rest;
        }, record);
      }
      /** `[Json]` Omits compositing symbols from this schema */
      Strict(schema) {
        return JSON.parse(JSON.stringify(schema));
      }
    };
    exports.TypeBuilder = TypeBuilder;
    var JsonTypeBuilder = class extends TypeBuilder {
      // ------------------------------------------------------------------------
      // Modifiers
      // ------------------------------------------------------------------------
      /** `[Json]` Creates a Readonly and Optional property */
      ReadonlyOptional(schema) {
        return this.Readonly(this.Optional(schema));
      }
      /** `[Json]` Creates a Readonly property */
      Readonly(schema) {
        return { ...TypeClone.Type(schema), [exports.Readonly]: "Readonly" };
      }
      /** `[Json]` Creates an Optional property */
      Optional(schema) {
        return { ...TypeClone.Type(schema), [exports.Optional]: "Optional" };
      }
      // ------------------------------------------------------------------------
      // Types
      // ------------------------------------------------------------------------
      /** `[Json]` Creates an Any type */
      Any(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Any" });
      }
      /** `[Json]` Creates an Array type */
      Array(schema, options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Array", type: "array", items: TypeClone.Type(schema) });
      }
      /** `[Json]` Creates a Boolean type */
      Boolean(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Boolean", type: "boolean" });
      }
      /** `[Json]` Intrinsic function to Capitalize LiteralString types */
      Capitalize(schema, options = {}) {
        return { ...Intrinsic.Map(TypeClone.Type(schema), "Capitalize"), ...options };
      }
      /** `[Json]` Creates a Composite object type */
      Composite(objects, options) {
        const intersect = exports.Type.Intersect(objects, {});
        const keys = KeyResolver.ResolveKeys(intersect, { includePatterns: false });
        const properties = keys.reduce((acc, key) => ({ ...acc, [key]: exports.Type.Index(intersect, [key]) }), {});
        return exports.Type.Object(properties, options);
      }
      /** `[Json]` Creates a Enum type */
      Enum(item, options = {}) {
        if (ValueGuard.IsUndefined(item))
          return this.Throw("Enum undefined or empty");
        const values1 = Object.getOwnPropertyNames(item).filter((key) => isNaN(key)).map((key) => item[key]);
        const values2 = [...new Set(values1)];
        const anyOf = values2.map((value) => exports.Type.Literal(value));
        return this.Union(anyOf, { ...options, [exports.Hint]: "Enum" });
      }
      /** `[Json]` Creates a Conditional type */
      Extends(left, right, trueType, falseType, options = {}) {
        switch (TypeExtends.Extends(left, right)) {
          case TypeExtendsResult.Union:
            return this.Union([TypeClone.Type(trueType, options), TypeClone.Type(falseType, options)]);
          case TypeExtendsResult.True:
            return TypeClone.Type(trueType, options);
          case TypeExtendsResult.False:
            return TypeClone.Type(falseType, options);
        }
      }
      /** `[Json]` Constructs a type by excluding from unionType all union members that are assignable to excludedMembers */
      Exclude(unionType, excludedMembers, options = {}) {
        return TypeGuard.TTemplateLiteral(unionType) ? this.Exclude(TemplateLiteralResolver.Resolve(unionType), excludedMembers, options) : TypeGuard.TTemplateLiteral(excludedMembers) ? this.Exclude(unionType, TemplateLiteralResolver.Resolve(excludedMembers), options) : TypeGuard.TUnion(unionType) ? (() => {
          const narrowed = unionType.anyOf.filter((inner) => TypeExtends.Extends(inner, excludedMembers) === TypeExtendsResult.False);
          return narrowed.length === 1 ? TypeClone.Type(narrowed[0], options) : this.Union(narrowed, options);
        })() : TypeExtends.Extends(unionType, excludedMembers) !== TypeExtendsResult.False ? this.Never(options) : TypeClone.Type(unionType, options);
      }
      /** `[Json]` Constructs a type by extracting from type all union members that are assignable to union */
      Extract(type, union, options = {}) {
        return TypeGuard.TTemplateLiteral(type) ? this.Extract(TemplateLiteralResolver.Resolve(type), union, options) : TypeGuard.TTemplateLiteral(union) ? this.Extract(type, TemplateLiteralResolver.Resolve(union), options) : TypeGuard.TUnion(type) ? (() => {
          const narrowed = type.anyOf.filter((inner) => TypeExtends.Extends(inner, union) !== TypeExtendsResult.False);
          return narrowed.length === 1 ? TypeClone.Type(narrowed[0], options) : this.Union(narrowed, options);
        })() : TypeExtends.Extends(type, union) !== TypeExtendsResult.False ? TypeClone.Type(type, options) : this.Never(options);
      }
      /** `[Json]` Returns an Indexed property type for the given keys */
      Index(schema, unresolved, options = {}) {
        return TypeGuard.TArray(schema) && TypeGuard.TNumber(unresolved) ? (() => {
          return TypeClone.Type(schema.items, options);
        })() : TypeGuard.TTuple(schema) && TypeGuard.TNumber(unresolved) ? (() => {
          const items = ValueGuard.IsUndefined(schema.items) ? [] : schema.items;
          const cloned = items.map((schema2) => TypeClone.Type(schema2));
          return this.Union(cloned, options);
        })() : (() => {
          const keys = KeyArrayResolver.Resolve(unresolved);
          const clone = TypeClone.Type(schema);
          return IndexedAccessor.Resolve(clone, keys, options);
        })();
      }
      /** `[Json]` Creates an Integer type */
      Integer(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Integer", type: "integer" });
      }
      /** `[Json]` Creates an Intersect type */
      Intersect(allOf, options = {}) {
        if (allOf.length === 0)
          return exports.Type.Never();
        if (allOf.length === 1)
          return TypeClone.Type(allOf[0], options);
        if (allOf.some((schema) => TypeGuard.TTransform(schema)))
          this.Throw("Cannot intersect transform types");
        const objects = allOf.every((schema) => TypeGuard.TObject(schema));
        const cloned = TypeClone.Rest(allOf);
        const clonedUnevaluatedProperties = TypeGuard.TSchema(options.unevaluatedProperties) ? { unevaluatedProperties: TypeClone.Type(options.unevaluatedProperties) } : {};
        return options.unevaluatedProperties === false || TypeGuard.TSchema(options.unevaluatedProperties) || objects ? this.Create({ ...options, ...clonedUnevaluatedProperties, [exports.Kind]: "Intersect", type: "object", allOf: cloned }) : this.Create({ ...options, ...clonedUnevaluatedProperties, [exports.Kind]: "Intersect", allOf: cloned });
      }
      /** `[Json]` Creates a KeyOf type */
      KeyOf(schema, options = {}) {
        return TypeGuard.TRecord(schema) ? (() => {
          const pattern = Object.getOwnPropertyNames(schema.patternProperties)[0];
          return pattern === exports.PatternNumberExact ? this.Number(options) : pattern === exports.PatternStringExact ? this.String(options) : this.Throw("Unable to resolve key type from Record key pattern");
        })() : TypeGuard.TTuple(schema) ? (() => {
          const items = ValueGuard.IsUndefined(schema.items) ? [] : schema.items;
          const literals = items.map((_, index) => exports.Type.Literal(index.toString()));
          return this.Union(literals, options);
        })() : TypeGuard.TArray(schema) ? (() => {
          return this.Number(options);
        })() : (() => {
          const keys = KeyResolver.ResolveKeys(schema, { includePatterns: false });
          if (keys.length === 0)
            return this.Never(options);
          const literals = keys.map((key) => this.Literal(key));
          return this.Union(literals, options);
        })();
      }
      /** `[Json]` Creates a Literal type */
      Literal(value, options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Literal", const: value, type: typeof value });
      }
      /** `[Json]` Intrinsic function to Lowercase LiteralString types */
      Lowercase(schema, options = {}) {
        return { ...Intrinsic.Map(TypeClone.Type(schema), "Lowercase"), ...options };
      }
      /** `[Json]` Creates a Never type */
      Never(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Never", not: {} });
      }
      /** `[Json]` Creates a Not type */
      Not(schema, options) {
        return this.Create({ ...options, [exports.Kind]: "Not", not: TypeClone.Type(schema) });
      }
      /** `[Json]` Creates a Null type */
      Null(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Null", type: "null" });
      }
      /** `[Json]` Creates a Number type */
      Number(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Number", type: "number" });
      }
      /** `[Json]` Creates an Object type */
      Object(properties, options = {}) {
        const propertyKeys = Object.getOwnPropertyNames(properties);
        const optionalKeys = propertyKeys.filter((key) => TypeGuard.TOptional(properties[key]));
        const requiredKeys = propertyKeys.filter((name) => !optionalKeys.includes(name));
        const clonedAdditionalProperties = TypeGuard.TSchema(options.additionalProperties) ? { additionalProperties: TypeClone.Type(options.additionalProperties) } : {};
        const clonedProperties = propertyKeys.reduce((acc, key) => ({ ...acc, [key]: TypeClone.Type(properties[key]) }), {});
        return requiredKeys.length > 0 ? this.Create({ ...options, ...clonedAdditionalProperties, [exports.Kind]: "Object", type: "object", properties: clonedProperties, required: requiredKeys }) : this.Create({ ...options, ...clonedAdditionalProperties, [exports.Kind]: "Object", type: "object", properties: clonedProperties });
      }
      /** `[Json]` Constructs a type whose keys are omitted from the given type */
      Omit(schema, unresolved, options = {}) {
        const keys = KeyArrayResolver.Resolve(unresolved);
        return ObjectMap.Map(this.Discard(TypeClone.Type(schema), ["$id", exports.Transform]), (object) => {
          if (ValueGuard.IsArray(object.required)) {
            object.required = object.required.filter((key) => !keys.includes(key));
            if (object.required.length === 0)
              delete object.required;
          }
          for (const key of Object.getOwnPropertyNames(object.properties)) {
            if (keys.includes(key))
              delete object.properties[key];
          }
          return this.Create(object);
        }, options);
      }
      /** `[Json]` Constructs a type where all properties are optional */
      Partial(schema, options = {}) {
        return ObjectMap.Map(this.Discard(TypeClone.Type(schema), ["$id", exports.Transform]), (object) => {
          const properties = Object.getOwnPropertyNames(object.properties).reduce((acc, key) => {
            return { ...acc, [key]: this.Optional(object.properties[key]) };
          }, {});
          return this.Object(
            properties,
            this.Discard(object, ["required"])
            /* object used as options to retain other constraints */
          );
        }, options);
      }
      /** `[Json]` Constructs a type whose keys are picked from the given type */
      Pick(schema, unresolved, options = {}) {
        const keys = KeyArrayResolver.Resolve(unresolved);
        return ObjectMap.Map(this.Discard(TypeClone.Type(schema), ["$id", exports.Transform]), (object) => {
          if (ValueGuard.IsArray(object.required)) {
            object.required = object.required.filter((key) => keys.includes(key));
            if (object.required.length === 0)
              delete object.required;
          }
          for (const key of Object.getOwnPropertyNames(object.properties)) {
            if (!keys.includes(key))
              delete object.properties[key];
          }
          return this.Create(object);
        }, options);
      }
      /** `[Json]` Creates a Record type */
      Record(key, schema, options = {}) {
        return TypeGuard.TTemplateLiteral(key) ? (() => {
          const expression = TemplateLiteralParser.ParseExact(key.pattern);
          return TemplateLiteralFinite.Check(expression) ? this.Object([...TemplateLiteralGenerator.Generate(expression)].reduce((acc, key2) => ({ ...acc, [key2]: TypeClone.Type(schema) }), {}), options) : this.Create({ ...options, [exports.Kind]: "Record", type: "object", patternProperties: { [key.pattern]: TypeClone.Type(schema) } });
        })() : TypeGuard.TUnion(key) ? (() => {
          const union = UnionResolver.Resolve(key);
          if (TypeGuard.TUnionLiteral(union)) {
            const properties = union.anyOf.reduce((acc, literal) => ({ ...acc, [literal.const]: TypeClone.Type(schema) }), {});
            return this.Object(properties, { ...options, [exports.Hint]: "Record" });
          } else
            this.Throw("Record key of type union contains non-literal types");
        })() : TypeGuard.TLiteral(key) ? (() => {
          return ValueGuard.IsString(key.const) || ValueGuard.IsNumber(key.const) ? this.Object({ [key.const]: TypeClone.Type(schema) }, options) : this.Throw("Record key of type literal is not of type string or number");
        })() : TypeGuard.TInteger(key) || TypeGuard.TNumber(key) ? (() => {
          return this.Create({ ...options, [exports.Kind]: "Record", type: "object", patternProperties: { [exports.PatternNumberExact]: TypeClone.Type(schema) } });
        })() : TypeGuard.TString(key) ? (() => {
          const pattern = ValueGuard.IsUndefined(key.pattern) ? exports.PatternStringExact : key.pattern;
          return this.Create({ ...options, [exports.Kind]: "Record", type: "object", patternProperties: { [pattern]: TypeClone.Type(schema) } });
        })() : this.Never();
      }
      /** `[Json]` Creates a Recursive type */
      Recursive(callback, options = {}) {
        if (ValueGuard.IsUndefined(options.$id))
          options.$id = `T${TypeOrdinal++}`;
        const thisType = callback({ [exports.Kind]: "This", $ref: `${options.$id}` });
        thisType.$id = options.$id;
        return this.Create({ ...options, [exports.Hint]: "Recursive", ...thisType });
      }
      /** `[Json]` Creates a Ref type. */
      Ref(unresolved, options = {}) {
        if (ValueGuard.IsString(unresolved))
          return this.Create({ ...options, [exports.Kind]: "Ref", $ref: unresolved });
        if (ValueGuard.IsUndefined(unresolved.$id))
          this.Throw("Reference target type must specify an $id");
        return this.Create({ ...options, [exports.Kind]: "Ref", $ref: unresolved.$id });
      }
      /** `[Json]` Constructs a type where all properties are required */
      Required(schema, options = {}) {
        return ObjectMap.Map(this.Discard(TypeClone.Type(schema), ["$id", exports.Transform]), (object) => {
          const properties = Object.getOwnPropertyNames(object.properties).reduce((acc, key) => {
            return { ...acc, [key]: this.Discard(object.properties[key], [exports.Optional]) };
          }, {});
          return this.Object(
            properties,
            object
            /* object used as options to retain other constraints  */
          );
        }, options);
      }
      /** `[Json]` Extracts interior Rest elements from Tuple, Intersect and Union types */
      Rest(schema) {
        return TypeGuard.TTuple(schema) && !ValueGuard.IsUndefined(schema.items) ? TypeClone.Rest(schema.items) : TypeGuard.TIntersect(schema) ? TypeClone.Rest(schema.allOf) : TypeGuard.TUnion(schema) ? TypeClone.Rest(schema.anyOf) : [];
      }
      /** `[Json]` Creates a String type */
      String(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "String", type: "string" });
      }
      /** `[Json]` Creates a TemplateLiteral type */
      TemplateLiteral(unresolved, options = {}) {
        const pattern = ValueGuard.IsString(unresolved) ? TemplateLiteralPattern.Create(TemplateLiteralDslParser.Parse(unresolved)) : TemplateLiteralPattern.Create(unresolved);
        return this.Create({ ...options, [exports.Kind]: "TemplateLiteral", type: "string", pattern });
      }
      /** `[Json]` Creates a Transform type */
      Transform(schema) {
        return new TransformDecodeBuilder(schema);
      }
      /** `[Json]` Creates a Tuple type */
      Tuple(items, options = {}) {
        const [additionalItems, minItems, maxItems] = [false, items.length, items.length];
        const clonedItems = TypeClone.Rest(items);
        const schema = items.length > 0 ? { ...options, [exports.Kind]: "Tuple", type: "array", items: clonedItems, additionalItems, minItems, maxItems } : { ...options, [exports.Kind]: "Tuple", type: "array", minItems, maxItems };
        return this.Create(schema);
      }
      /** `[Json]` Intrinsic function to Uncapitalize LiteralString types */
      Uncapitalize(schema, options = {}) {
        return { ...Intrinsic.Map(TypeClone.Type(schema), "Uncapitalize"), ...options };
      }
      /** `[Json]` Creates a Union type */
      Union(union, options = {}) {
        return TypeGuard.TTemplateLiteral(union) ? TemplateLiteralResolver.Resolve(union) : (() => {
          const anyOf = union;
          if (anyOf.length === 0)
            return this.Never(options);
          if (anyOf.length === 1)
            return this.Create(TypeClone.Type(anyOf[0], options));
          const clonedAnyOf = TypeClone.Rest(anyOf);
          return this.Create({ ...options, [exports.Kind]: "Union", anyOf: clonedAnyOf });
        })();
      }
      /** `[Json]` Creates an Unknown type */
      Unknown(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Unknown" });
      }
      /** `[Json]` Creates a Unsafe type that will infers as the generic argument T */
      Unsafe(options = {}) {
        return this.Create({ ...options, [exports.Kind]: options[exports.Kind] || "Unsafe" });
      }
      /** `[Json]` Intrinsic function to Uppercase LiteralString types */
      Uppercase(schema, options = {}) {
        return { ...Intrinsic.Map(TypeClone.Type(schema), "Uppercase"), ...options };
      }
    };
    exports.JsonTypeBuilder = JsonTypeBuilder;
    var JavaScriptTypeBuilder = class extends JsonTypeBuilder {
      /** `[JavaScript]` Creates a AsyncIterator type */
      AsyncIterator(items, options = {}) {
        return this.Create({ ...options, [exports.Kind]: "AsyncIterator", type: "AsyncIterator", items: TypeClone.Type(items) });
      }
      /** `[JavaScript]` Constructs a type by recursively unwrapping Promise types */
      Awaited(schema, options = {}) {
        const Unwrap = (rest) => rest.length > 0 ? (() => {
          const [L, ...R] = rest;
          return [this.Awaited(L), ...Unwrap(R)];
        })() : rest;
        return TypeGuard.TIntersect(schema) ? exports.Type.Intersect(Unwrap(schema.allOf)) : TypeGuard.TUnion(schema) ? exports.Type.Union(Unwrap(schema.anyOf)) : TypeGuard.TPromise(schema) ? this.Awaited(schema.item) : TypeClone.Type(schema, options);
      }
      /** `[JavaScript]` Creates a BigInt type */
      BigInt(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "BigInt", type: "bigint" });
      }
      /** `[JavaScript]` Extracts the ConstructorParameters from the given Constructor type */
      ConstructorParameters(schema, options = {}) {
        return this.Tuple([...schema.parameters], { ...options });
      }
      /** `[JavaScript]` Creates a Constructor type */
      Constructor(parameters, returns, options) {
        const [clonedParameters, clonedReturns] = [TypeClone.Rest(parameters), TypeClone.Type(returns)];
        return this.Create({ ...options, [exports.Kind]: "Constructor", type: "Constructor", parameters: clonedParameters, returns: clonedReturns });
      }
      /** `[JavaScript]` Creates a Date type */
      Date(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Date", type: "Date" });
      }
      /** `[JavaScript]` Creates a Function type */
      Function(parameters, returns, options) {
        const [clonedParameters, clonedReturns] = [TypeClone.Rest(parameters), TypeClone.Type(returns)];
        return this.Create({ ...options, [exports.Kind]: "Function", type: "Function", parameters: clonedParameters, returns: clonedReturns });
      }
      /** `[JavaScript]` Extracts the InstanceType from the given Constructor type */
      InstanceType(schema, options = {}) {
        return TypeClone.Type(schema.returns, options);
      }
      /** `[JavaScript]` Creates an Iterator type */
      Iterator(items, options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Iterator", type: "Iterator", items: TypeClone.Type(items) });
      }
      /** `[JavaScript]` Extracts the Parameters from the given Function type */
      Parameters(schema, options = {}) {
        return this.Tuple(schema.parameters, { ...options });
      }
      /** `[JavaScript]` Creates a Promise type */
      Promise(item, options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Promise", type: "Promise", item: TypeClone.Type(item) });
      }
      /** `[Extended]` Creates a String type */
      RegExp(unresolved, options = {}) {
        const pattern = ValueGuard.IsString(unresolved) ? unresolved : unresolved.source;
        return this.Create({ ...options, [exports.Kind]: "String", type: "string", pattern });
      }
      /**
       * @deprecated Use `Type.RegExp`
       */
      RegEx(regex, options = {}) {
        return this.RegExp(regex, options);
      }
      /** `[JavaScript]` Extracts the ReturnType from the given Function type */
      ReturnType(schema, options = {}) {
        return TypeClone.Type(schema.returns, options);
      }
      /** `[JavaScript]` Creates a Symbol type */
      Symbol(options) {
        return this.Create({ ...options, [exports.Kind]: "Symbol", type: "symbol" });
      }
      /** `[JavaScript]` Creates a Undefined type */
      Undefined(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Undefined", type: "undefined" });
      }
      /** `[JavaScript]` Creates a Uint8Array type */
      Uint8Array(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Uint8Array", type: "Uint8Array" });
      }
      /** `[JavaScript]` Creates a Void type */
      Void(options = {}) {
        return this.Create({ ...options, [exports.Kind]: "Void", type: "void" });
      }
    };
    exports.JavaScriptTypeBuilder = JavaScriptTypeBuilder;
    exports.JsonType = new JsonTypeBuilder();
    exports.Type = new JavaScriptTypeBuilder();
  }
});

// src/settings.ts
var import_typebox = __toESM(require_typebox(), 1);
var pathPatternString = import_typebox.Type.String({
  // for legacy reasions locale can be specified as well
  pattern: ".*\\{languageTag|locale\\}.*\\.json$",
  examples: ["./messages/{locale}.json", "./i18n/{locale}.json"],
  title: "Path to language files",
  description: "Specify the pathPattern to locate resource files in your repository. It must include `{locale}` and end with `.json`."
});
var pathPatternArray = import_typebox.Type.Array(pathPatternString, {
  title: "Paths to language files",
  description: "Specify multiple pathPatterns to locate resource files in your repository. Each must include `{locale}` and end with `.json`."
});
var sort = import_typebox.Type.Optional(
  import_typebox.Type.Union([import_typebox.Type.Literal("asc"), import_typebox.Type.Literal("desc")], {
    title: "Sort keys",
    description: "Sort message keys when writing files."
  })
);
var PluginSettings = import_typebox.Type.Object({
  pathPattern: import_typebox.Type.Union([pathPatternString, pathPatternArray]),
  sort
});

// src/v2/parsing/serializePattern.ts
var serializedPattern = (pattern) => {
  return pattern.map((node) => {
    switch (node.type) {
      case "Text":
        return node.value;
      case "VariableReference":
        return `{${node.name}}`;
    }
  }).join("");
};

// src/v2/parsing/serializeMessage.ts
var serializeMessage = (message) => {
  const result = {};
  for (const variant of message.variants) {
    if (result[variant.languageTag] !== void 0) {
      throw new Error(
        `The message "${message.id}" has multiple variants for the language tag "${variant.languageTag}". The inlang-message-format plugin does not support multiple variants for the same language tag at the moment.`
      );
    }
    result[variant.languageTag] = serializedPattern(variant.pattern);
  }
  return result;
};

// src/v2/parsing/parsePattern.ts
var parsePattern = (pattern) => {
  const regex = /\{([^}]+)\}/g;
  let match;
  let lastIndex = 0;
  const result = [];
  while ((match = regex.exec(pattern)) !== null) {
    const name = match[1];
    const textBefore = pattern.slice(lastIndex, match.index);
    if (textBefore.length > 0) {
      result.push({ type: "Text", value: textBefore });
    }
    result.push({ type: "VariableReference", name });
    lastIndex = match.index + match[0].length;
  }
  const textAfter = pattern.slice(Math.max(0, lastIndex));
  if (textAfter.length > 0) {
    result.push({ type: "Text", value: textAfter });
  }
  return result;
};

// src/v2/parsing/parseMessage.ts
var parseMessage = (args) => {
  return {
    id: args.key,
    alias: {},
    selectors: [],
    variants: [
      {
        languageTag: args.languageTag,
        match: [],
        pattern: parsePattern(args.value)
      }
    ]
  };
};

// src/v2/plugin.ts
var pluginId = "plugin.inlang.messageFormat";
var plugin = {
  id: pluginId,
  displayName: "Inlang Message Format",
  description: "A plugin for the inlang SDK that uses a JSON file per language tag to store translations.",
  key: "inlang-message-format",
  settingsSchema: PluginSettings,
  loadMessages: async ({ settings, nodeishFs }) => {
    await maybeMigrateToV2({ settings, nodeishFs });
    const result = {};
    for (const tag of settings.languageTags) {
      try {
        const file = await nodeishFs.readFile(
          settings["plugin.inlang.messageFormat"].pathPattern.replace(
            "{languageTag}",
            tag
          ),
          {
            encoding: "utf-8"
          }
        );
        const json = JSON.parse(file);
        for (const key in json) {
          if (key === "$schema") {
            continue;
          } else if (result[key]) {
            result[key].variants = [
              // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
              ...result[key].variants,
              ...parseMessage({ key, value: json[key], languageTag: tag }).variants
            ];
          } else {
            result[key] = parseMessage({
              key,
              value: json[key],
              languageTag: tag
            });
          }
        }
      } catch (error) {
        if (error?.code !== "ENOENT") {
          throw error;
        }
      }
    }
    return Object.values(result);
  },
  saveMessages: async ({ settings, nodeishFs, messages }) => {
    const result = {};
    for (const message of messages) {
      const serialized = serializeMessage(message);
      for (const [languageTag, value] of Object.entries(serialized)) {
        if (result[languageTag] === void 0) {
          result[languageTag] = {};
        }
        result[languageTag][message.id] = value;
      }
    }
    for (const [languageTag, messages2] of Object.entries(result)) {
      const path = settings["plugin.inlang.messageFormat"].pathPattern.replace(
        "{languageTag}",
        languageTag
      );
      await createDirectoryIfNotExits({ path, nodeishFs });
      await nodeishFs.writeFile(
        settings["plugin.inlang.messageFormat"].pathPattern.replace(
          "{languageTag}",
          languageTag
        ),
        // default to tab indentation
        // PS sorry for anyone who reads this code
        ((data) => JSON.stringify(data, void 0, "	"))({
          $schema: "https://inlang.com/schema/inlang-message-format",
          ...messages2
        })
      );
    }
  }
};
var createDirectoryIfNotExits = async (args) => {
  try {
    await args.nodeishFs.mkdir(dirname(args.path), { recursive: true });
  } catch {
  }
};
function dirname(path) {
  if (path.length === 0) return ".";
  let code = path.charCodeAt(0);
  const hasRoot = code === 47;
  let end = -1;
  let matchedSlash = true;
  for (let i = path.length - 1; i >= 1; --i) {
    code = path.charCodeAt(i);
    if (code === 47) {
      if (!matchedSlash) {
        end = i;
        break;
      }
    } else {
      matchedSlash = false;
    }
  }
  if (end === -1) return hasRoot ? "/" : ".";
  if (hasRoot && end === 1) return "//";
  return path.slice(0, end);
}
var maybeMigrateToV2 = async (args) => {
  if (args.settings["plugin.inlang.messageFormat"].filePath == void 0) {
    return;
  }
  try {
    const file = await args.nodeishFs.readFile(
      args.settings["plugin.inlang.messageFormat"].filePath,
      {
        encoding: "utf-8"
      }
    );
    await plugin.saveMessages?.({
      messages: JSON.parse(file)["data"],
      nodeishFs: args.nodeishFs,
      settings: args.settings
    });
    console.log(
      "Migration to v2 of the inlang-message-format plugin was successful. Please delete the old messages.json file and the filePath property in the settings file of the project."
    );
  } catch {
  }
};

// src/import-export/toBeImportedFiles.ts
var toBeImportedFiles = async ({ settings }) => {
  const result = [];
  const pathPatterns = settings[PLUGIN_KEY]?.pathPattern ? Array.isArray(settings[PLUGIN_KEY].pathPattern) ? settings[PLUGIN_KEY].pathPattern : [settings[PLUGIN_KEY].pathPattern] : [];
  for (const pathPattern of pathPatterns) {
    for (const locale of settings.locales) {
      result.push({
        locale,
        path: pathPattern.replace(/{(locale|languageTag)}/, locale)
      });
    }
  }
  return result;
};

// ../../../node_modules/.pnpm/flat@6.0.1/node_modules/flat/index.js
function isBuffer(obj) {
  return obj && obj.constructor && typeof obj.constructor.isBuffer === "function" && obj.constructor.isBuffer(obj);
}
function keyIdentity(key) {
  return key;
}
function flatten(target, opts) {
  opts = opts || {};
  const delimiter = opts.delimiter || ".";
  const maxDepth = opts.maxDepth;
  const transformKey = opts.transformKey || keyIdentity;
  const output = {};
  function step(object, prev, currentDepth) {
    currentDepth = currentDepth || 1;
    Object.keys(object).forEach(function(key) {
      const value = object[key];
      const isarray = opts.safe && Array.isArray(value);
      const type = Object.prototype.toString.call(value);
      const isbuffer = isBuffer(value);
      const isobject = type === "[object Object]" || type === "[object Array]";
      const newKey = prev ? prev + delimiter + transformKey(key) : transformKey(key);
      if (!isarray && !isbuffer && isobject && Object.keys(value).length && (!opts.maxDepth || currentDepth < maxDepth)) {
        return step(value, newKey, currentDepth + 1);
      }
      output[newKey] = value;
    });
  }
  step(target);
  return output;
}
function unflatten(target, opts) {
  opts = opts || {};
  const delimiter = opts.delimiter || ".";
  const overwrite = opts.overwrite || false;
  const transformKey = opts.transformKey || keyIdentity;
  const result = {};
  const isbuffer = isBuffer(target);
  if (isbuffer || Object.prototype.toString.call(target) !== "[object Object]") {
    return target;
  }
  function getkey(key) {
    const parsedKey = Number(key);
    return isNaN(parsedKey) || key.indexOf(".") !== -1 || opts.object ? key : parsedKey;
  }
  function addKeys(keyPrefix, recipient, target2) {
    return Object.keys(target2).reduce(function(result2, key) {
      result2[keyPrefix + delimiter + key] = target2[key];
      return result2;
    }, recipient);
  }
  function isEmpty(val) {
    const type = Object.prototype.toString.call(val);
    const isArray = type === "[object Array]";
    const isObject = type === "[object Object]";
    if (!val) {
      return true;
    } else if (isArray) {
      return !val.length;
    } else if (isObject) {
      return !Object.keys(val).length;
    }
  }
  target = Object.keys(target).reduce(function(result2, key) {
    const type = Object.prototype.toString.call(target[key]);
    const isObject = type === "[object Object]" || type === "[object Array]";
    if (!isObject || isEmpty(target[key])) {
      result2[key] = target[key];
      return result2;
    } else {
      return addKeys(
        key,
        result2,
        flatten(target[key], opts)
      );
    }
  }, {});
  Object.keys(target).forEach(function(key) {
    const split = key.split(delimiter).map(transformKey);
    let key1 = getkey(split.shift());
    let key2 = getkey(split[0]);
    let recipient = result;
    while (key2 !== void 0) {
      if (key1 === "__proto__") {
        return;
      }
      const type = Object.prototype.toString.call(recipient[key1]);
      const isobject = type === "[object Object]" || type === "[object Array]";
      if (!overwrite && !isobject && typeof recipient[key1] !== "undefined") {
        return;
      }
      if (overwrite && !isobject || !overwrite && recipient[key1] == null) {
        recipient[key1] = typeof key2 === "number" && !opts.object ? [] : {};
      }
      recipient = recipient[key1];
      if (split.length > 0) {
        key1 = getkey(split.shift());
        key2 = getkey(split[0]);
      }
    }
    recipient[key1] = unflatten(target[key], opts);
  });
  return result;
}

// src/import-export/importFiles.ts
var importFiles = async ({
  files
}) => {
  const bundles = [];
  const messages = [];
  const variants = [];
  for (const file of files) {
    const json = JSON.parse(new TextDecoder().decode(file.content));
    const flattened = flatten(json, { safe: true });
    for (const key in flattened) {
      if (key === "$schema") {
        continue;
      }
      const result = parseBundle(key, file.locale, flattened[key]);
      messages.push(result.message);
      variants.push(...result.variants);
      const existingBundle = bundles.find((b) => b.id === result.bundle.id);
      if (existingBundle === void 0) {
        bundles.push(result.bundle);
      } else {
        existingBundle.declarations = unique([
          ...existingBundle.declarations,
          ...result.bundle.declarations
        ]);
      }
    }
  }
  return { bundles, messages, variants };
};
function parseBundle(key, locale, value) {
  const parsed = parseVariants(key, locale, value);
  const declarations = unique(parsed.declarations);
  const selectors = unique(parsed.selectors);
  const undeclaredSelectors = selectors.filter(
    (selector) => declarations.find((d) => d.name === selector.name) === void 0
  );
  for (const undeclaredSelector of undeclaredSelectors) {
    declarations.push({
      type: "input-variable",
      name: undeclaredSelector.name
    });
  }
  return {
    bundle: {
      id: key,
      declarations
    },
    message: {
      bundleId: key,
      selectors,
      locale
    },
    variants: parsed.variants
  };
}
function parseVariants(bundleId, locale, value) {
  if (typeof value === "string") {
    const parsed = parsePattern2(value);
    return {
      variants: [
        {
          messageBundleId: bundleId,
          messageLocale: locale,
          matches: [],
          pattern: parsed.pattern
        }
      ],
      // legacy reasons that input variables are derived from the pattern
      declarations: parsed.declarations,
      selectors: []
    };
  }
  const complexMessage = value[0];
  const variants = [];
  const selectors = (complexMessage["selectors"] ?? []).map((name) => ({
    type: "variable-reference",
    name
  }));
  const declarations = /* @__PURE__ */ new Set();
  for (const declaration of complexMessage["declarations"] ?? []) {
    declarations.add(parseDeclaration(declaration));
  }
  const detectedSelectors = /* @__PURE__ */ new Set();
  for (const [match, pattern] of Object.entries(complexMessage["match"])) {
    const parsed = parsePattern2(pattern);
    const parsedMatches = parseMatches(match);
    for (const declaration of parsed.declarations) {
      let isDuplicate = false;
      for (const existingDeclaration of declarations) {
        if (existingDeclaration.name === declaration.name) {
          isDuplicate = true;
          break;
        }
      }
      if (isDuplicate) {
        break;
      }
      declarations.add(declaration);
    }
    for (const selector of parsedMatches.selectors) {
      detectedSelectors.add(selector);
    }
    variants.push({
      messageBundleId: bundleId,
      messageLocale: locale,
      matches: parsedMatches.matches,
      pattern: parsed.pattern
    });
  }
  return {
    variants,
    declarations: Array.from(declarations),
    selectors: unique([...selectors, ...Array.from(detectedSelectors)])
  };
}
function parsePattern2(value) {
  const pattern = [];
  const declarations = [];
  let buffer = "";
  const flushBuffer = () => {
    if (buffer.length > 0) {
      pattern.push({ type: "text", value: buffer });
      buffer = "";
    }
  };
  for (let index = 0; index < value.length; index += 1) {
    const char = value[index];
    if (char === "\\") {
      const next = value[index + 1];
      if (next === "{" || next === "}" || next === "\\") {
        buffer += next;
        index += 1;
        continue;
      }
      buffer += char;
      continue;
    }
    if (char === "{") {
      let variableName = "";
      let closingIndex = -1;
      for (let cursor = index + 1; cursor < value.length; cursor += 1) {
        const current = value[cursor];
        if (current === "}") {
          closingIndex = cursor;
          break;
        }
        variableName += current;
      }
      if (closingIndex === -1) {
        buffer += char;
        continue;
      }
      flushBuffer();
      declarations.push({
        type: "input-variable",
        name: variableName
      });
      pattern.push({
        type: "expression",
        arg: { type: "variable-reference", name: variableName }
      });
      index = closingIndex;
      continue;
    }
    buffer += char;
  }
  flushBuffer();
  return {
    declarations,
    pattern
  };
}
function parseMatches(value) {
  const stripped = value.replace(" ", "");
  const matches = [];
  const selectors = [];
  const parts = stripped.split(",");
  for (const part of parts) {
    const [key, value2] = part.split("=");
    if (!key || !value2) {
      continue;
    }
    if (value2 === "*") {
      matches.push({
        type: "catchall-match",
        key
      });
    } else {
      matches.push({
        type: "literal-match",
        key,
        value: value2
      });
    }
    selectors.push({
      type: "variable-reference",
      name: key
    });
  }
  return { matches, selectors };
}
var unique = (arr) => [...new Set(arr.map((item) => JSON.stringify(item)))].map(
  (item) => JSON.parse(item)
);
function parseDeclaration(value) {
  if (value.startsWith("input")) {
    return {
      type: "input-variable",
      name: value.slice(6).trim()
    };
  } else if (value.startsWith("local")) {
    const match = value.match(/local (\w+) = (\w+): (\w+)(.*)/);
    const [, name, ref, fn, optionsString] = match;
    const options = optionsString?.trim().split(/\s+/).map((pair) => {
      const [key, value2] = pair.split("=");
      return key && value2 ? {
        name: key,
        value: { type: "literal", value: value2 }
      } : null;
    }).filter(Boolean);
    return {
      type: "local-variable",
      name: name.trim(),
      value: {
        type: "expression",
        arg: {
          type: "variable-reference",
          name: ref.trim()
        },
        annotation: fn ? {
          type: "function-reference",
          name: fn.trim(),
          options: options ?? []
        } : void 0
      }
    };
  }
  throw new Error("Unsupported declaration type");
}

// src/utils/sortKeys.ts
var compare = (direction) => (a, b) => direction === "desc" ? b.localeCompare(a) : a.localeCompare(b);
var sortObjectKeys = (value, direction) => {
  if (Array.isArray(value)) {
    return value.map((entry) => sortObjectKeys(entry, direction));
  }
  if (value === null || typeof value !== "object") {
    return value;
  }
  const sorted = {};
  for (const [key, entry] of Object.entries(value).sort(
    (a, b) => compare(direction)(a[0], b[0])
  )) {
    sorted[key] = sortObjectKeys(entry, direction);
  }
  return sorted;
};
var sortMessageKeys = (value, direction) => {
  const sorted = {};
  for (const [key, entry] of Object.entries(value).sort(
    (a, b) => compare(direction)(a[0], b[0])
  )) {
    sorted[key] = sortObjectKeys(entry, direction);
  }
  return sorted;
};

// src/import-export/exportFiles.ts
var exportFiles = async ({
  bundles,
  messages,
  variants,
  settings
}) => {
  const files = {};
  for (const message of messages) {
    const bundle = bundles.find((b) => b.id === message.bundleId);
    const variantsOfMessage = [
      ...variants.reduce((r, v) => {
        if (v.messageId === message.id) r.set(JSON.stringify(v.matches), v);
        return r;
      }, /* @__PURE__ */ new Map()).values()
    ];
    files[message.locale] = {
      ...files[message.locale],
      ...serializeMessage2(bundle, message, variantsOfMessage)
    };
  }
  const result = [];
  for (const locale in files) {
    const sortDirection = settings?.["plugin.inlang.messageFormat"]?.sort ?? void 0;
    const unflattened = unflatten(files[locale]);
    const sortedMessages = sortDirection ? sortMessageKeys(unflattened, sortDirection) : unflattened;
    result.push({
      locale,
      // beautify the json
      content: new TextEncoder().encode(
        JSON.stringify(
          {
            // increase DX by providing auto complete in IDEs
            $schema: "https://inlang.com/schema/inlang-message-format",
            ...sortedMessages
          },
          void 0,
          "	"
        )
      ),
      name: locale + ".json"
    });
  }
  return result;
};
function serializeMessage2(bundle, message, variants) {
  const key = message.bundleId;
  const value = serializeVariants(bundle, message, variants);
  return { [key]: value };
}
function serializeVariants(bundle, message, variants) {
  if (variants.length === 1) {
    if (message.selectors.length === 0 && bundle.declarations.some((d) => d.type !== "input-variable") === false) {
      return serializePattern(variants[0].pattern);
    }
  }
  const entries = [];
  for (const variant of variants) {
    if (variant.matches.length === 0) {
      for (const part of variant.pattern) {
        if (part.type === "expression" && part.arg.type === "variable-reference") {
          variant.matches.push({ key: part.arg.name, type: "catchall-match" });
        }
      }
    }
    const pattern = serializePattern(variant.pattern);
    const match = serializeMatcher(variant.matches);
    entries.push([match, pattern]);
  }
  return [
    {
      // naively adding all declarations, even if unused in the variants
      // can be optimized later.
      declarations: bundle.declarations.sort((a, b) => a.name.localeCompare(b.name)).map(serializeDeclaration).sort(),
      selectors: message.selectors.map((s) => s.name).sort(),
      match: Object.fromEntries(entries)
    }
  ];
}
function serializePattern(pattern) {
  let result = "";
  for (const part of pattern) {
    if (part.type === "text") {
      result += escapePatternText(part.value);
    } else if (part.arg.type === "variable-reference") {
      result += `{${part.arg.name}}`;
    } else {
      throw new Error("Unsupported expression type");
    }
  }
  return result;
}
function escapePatternText(value) {
  return value.replace(/\\/g, "\\\\").replace(/{/g, "\\{").replace(/}/g, "\\}");
}
function serializeMatcher(matches) {
  const parts = matches.sort((a, b) => a.key.localeCompare(b.key)).map(
    (match) => match.type === "literal-match" ? `${match.key}=${match.value}` : `${match.key}=*`
  );
  return parts.join(", ");
}
function serializeDeclaration(declaration) {
  if (declaration.type === "input-variable") {
    return `input ${declaration.name}`;
  } else if (declaration.type === "local-variable") {
    let result = "";
    if (declaration.value.arg.type === "variable-reference") {
      result = `local ${declaration.name} = ${declaration.value.arg.name}`;
    } else if (declaration.value.arg.type === "literal") {
      result = `local ${declaration.name} = "${declaration.value.arg.value}"`;
    }
    if (declaration.value.annotation) {
      result += `: ${declaration.value.annotation.name}`;
    }
    if (declaration.value.annotation?.options) {
      for (const option of declaration.value?.annotation?.options ?? []) {
        if (option.value.type !== "literal") {
          throw new Error("Unsupported option type");
        }
        result += ` ${option.name}=${option.value.value}`;
      }
    }
    return result;
  }
  throw new Error("Unsupported declaration type");
}

// src/plugin.ts
var PLUGIN_KEY = "plugin.inlang.messageFormat";
var plugin2 = {
  key: PLUGIN_KEY,
  // legacy v2 stuff for backwards compatibility
  // given that most people don't have a major version
  // pinning in their settings
  id: plugin.id,
  // @ts-expect-error - displayName is not in the v2 plugin
  displayName: plugin.displayName,
  // @ts-expect-error - description is not in the v2 plugin
  description: plugin.description,
  loadMessages: plugin.loadMessages,
  saveMessages: plugin.saveMessages,
  settingsSchema: PluginSettings,
  toBeImportedFiles,
  importFiles,
  exportFiles
};

// src/index.ts
var index_default = plugin2;
export {
  index_default as default
};
