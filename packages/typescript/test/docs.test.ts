import assert from "node:assert/strict";
import { test } from "node:test";
import ts from "typescript";

// Every name the package exports carries a TSDoc comment, so a caller's
// editor says what the thing is without a trip to the source. The comment
// has to be the /** */ form: a // comment is stripped from the declarations
// tsc emits, and a consumer never sees it.
//
// The export list in src/index.ts is the input. Each name is followed to its
// declaration, so a re-export of a bare declaration fails on the declaration
// and the refusal names the line the sentence goes on.

const root = new URL("../", import.meta.url);
const entry = new URL("src/index.ts", root);

const configPath = ts.findConfigFile(ts.sys.getCurrentDirectory(), ts.sys.fileExists, "tsconfig.json");
assert.ok(configPath, "tsconfig.json is not beside the package");

const config = ts.readConfigFile(configPath, ts.sys.readFile);
assert.equal(config.error, undefined, "tsconfig.json did not parse");

const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, ts.sys.getCurrentDirectory());
const program = ts.createProgram([toPath(entry)], { ...parsed.options, noEmit: true });
const checker = program.getTypeChecker();

function toPath(url: URL): string {
  return decodeURIComponent(url.pathname).replace(/^\/([A-Za-z]:)/, "$1");
}

function relative(file: string): string {
  const base = toPath(root);
  return file.startsWith(base) ? file.slice(base.length) : file;
}

function hasTSDoc(declaration: ts.Declaration): boolean {
  return ts.getJSDocCommentsAndTags(declaration).some((node) => ts.isJSDoc(node) && node.comment !== undefined);
}

test("every exported name carries a TSDoc comment", () => {
  const source = program.getSourceFile(toPath(entry));
  assert.ok(source, "src/index.ts is not in the program");

  const moduleSymbol = checker.getSymbolAtLocation(source);
  assert.ok(moduleSymbol, "src/index.ts has no module symbol");

  const exported = checker.getExportsOfModule(moduleSymbol);
  assert.ok(exported.length >= 30, `the package exports ${exported.length} names, which is fewer than expected`);

  const gaps: string[] = [];

  for (const symbol of exported) {
    const resolved = symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol;
    const declarations = resolved.declarations ?? [];
    assert.ok(declarations.length > 0, `${symbol.name} resolves to no declaration`);

    for (const declaration of declarations) {
      if (hasTSDoc(declaration)) continue;
      const file = declaration.getSourceFile();
      const { line } = file.getLineAndCharacterOfPosition(declaration.getStart());
      gaps.push(
        `${relative(file.fileName)}:${line + 1} exported ${symbol.name} has no TSDoc comment. ` +
          `Write /** one sentence saying what it is */ above the declaration.`,
      );
    }
  }

  assert.equal(gaps.length, 0, `${gaps.length} exported names ship without a sentence:\n${gaps.join("\n")}`);
});
