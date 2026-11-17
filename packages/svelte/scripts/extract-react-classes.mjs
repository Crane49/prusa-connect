// Reads the React component sources and writes the class fixture that
// src/primitives/classes.test.ts compares the Svelte components against.
//
// The React tree is the design system, and the branch that holds it is going
// away. This script runs while that branch is still checked out somewhere, and
// the fixture it writes is what stays. It has no Makefile target, because a
// `-check` target would fail for good the day that branch is deleted.
//
//     node scripts/extract-react-classes.mjs \
//       --ui /tmp/pcl-main/frontend/src/next/components/ui \
//       --out src/primitives/classes.fixture.json
//
// `--only button,checkbox,dialog` reads a few components and prints the result
// instead of writing the file.
//
// It records, per component, a map from a `data-slot` name to the set of class
// names that can land on that slot. "Can land" is the point: a class that only
// appears under `variant: "destructive"` is still part of the component's
// vocabulary, so every branch of a `cva` variant map counts.
//
// A class on an element with no `data-slot` of its own goes to the nearest
// ancestor that has one. A `.tsx` file with no `data-slot` anywhere records
// one slot named after the file. A `.ts` file is a class recipe with no
// markup, and its exported strings are that one slot. The Svelte side reads
// its own sources by the same rules, so the two sets compare.
import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { basename, join } from 'node:path';

import ts from 'typescript';

/** @typedef {import('typescript').Node} Node */
/** @typedef {import('typescript').Expression} Expression */

const args = process.argv.slice(2);

/**
 * @param {string} name
 * @param {string} fallback
 * @returns {string}
 */
const option = (name, fallback) => {
  const at = args.indexOf(`--${name}`);
  return at === -1 ? fallback : (args[at + 1] ?? fallback);
};

const uiDir = option('ui', '/tmp/pcl-main/frontend/src/next/components/ui');
const outPath = option('out', 'src/primitives/classes.fixture.json');
const only = option('only', '');

/**
 * A class string becomes class names on whitespace. Tailwind writes a space
 * inside an arbitrary value as `_`, so there is nothing to protect here.
 *
 * @param {string} text
 * @returns {string[]}
 */
const names = (text) => text.split(/\s+/).filter(Boolean);

/**
 * Every class string reachable from an expression.
 *
 * `cva` is the one call that needs a rule of its own. Its second argument
 * holds `variants`, whose leaves are class strings, and `defaultVariants`,
 * whose leaves are variant names. Reading both would put `default` and `md`
 * in the fixture as if they were classes.
 *
 * @param {Node | undefined} node
 * @param {Map<string, Expression[]>} consts
 * @param {Set<string>} seen
 * @returns {string[]}
 */
const stringsOf = (node, consts, seen = new Set()) => {
  if (!node) return [];
  /**
   * @param {(Node | undefined)[]} list
   * @returns {string[]}
   */
  const parts = (list) => list.flatMap((item) => stringsOf(item, consts, seen));

  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return [node.text];
  }
  if (ts.isTemplateExpression(node)) {
    return [
      node.head.text,
      ...node.templateSpans.map((span) => span.literal.text),
    ];
  }
  if (
    ts.isParenthesizedExpression(node) ||
    ts.isAsExpression(node) ||
    ts.isSatisfiesExpression(node)
  ) {
    return stringsOf(node.expression, consts, seen);
  }
  if (ts.isArrayLiteralExpression(node)) return parts([...node.elements]);
  if (ts.isConditionalExpression(node)) {
    return parts([node.whenTrue, node.whenFalse]);
  }
  // `mono && 'font-mono'` puts a class on the right. The left is a condition,
  // and reading it turns `position === 'popper'` into a class called `popper`.
  if (ts.isBinaryExpression(node)) {
    const operator = node.operatorToken.kind;
    if (operator === ts.SyntaxKind.AmpersandAmpersandToken) {
      return stringsOf(node.right, consts, seen);
    }
    if (
      operator === ts.SyntaxKind.BarBarToken ||
      operator === ts.SyntaxKind.QuestionQuestionToken
    ) {
      return parts([node.left, node.right]);
    }
    return [];
  }
  if (ts.isObjectLiteralExpression(node)) {
    return parts(
      node.properties
        .filter((property) => ts.isPropertyAssignment(property))
        .map((property) => property.initializer),
    );
  }
  if (ts.isCallExpression(node)) {
    const callee = ts.isIdentifier(node.expression) ? node.expression.text : '';
    if (callee === 'cva') {
      const [base, options] = node.arguments;
      const variants =
        options && ts.isObjectLiteralExpression(options)
          ? options.properties
              .filter((property) => ts.isPropertyAssignment(property))
              .filter((property) => property.name.getText() === 'variants')
              .map((property) => property.initializer)
          : [];
      return parts([base, ...variants]);
    }
    // `cn(a, b)` joins classes, so both arguments are classes. Any other call
    // is a recipe reached through its name, and its arguments are the variant
    // keys to pick: reading them would record `default` and `md` as classes.
    const joiners = ['cn', 'clsx', 'classNames', 'twMerge'];
    if (joiners.includes(callee)) return parts([...node.arguments]);
    return stringsOf(node.expression, consts, seen);
  }
  // `const base = (className) => cn('size-4 shrink-0', className)`, the shape
  // control-icon.tsx and telemetry-icon.tsx share one recipe with.
  if (ts.isArrowFunction(node) || ts.isFunctionExpression(node)) {
    return stringsOf(node.body, consts, seen);
  }
  if (ts.isBlock(node)) return parts([...node.statements]);
  if (ts.isReturnStatement(node))
    return stringsOf(node.expression, consts, seen);
  if (ts.isIdentifier(node)) {
    const declared = consts.get(node.text);
    if (!declared || seen.has(node.text)) return [];
    seen.add(node.text);
    return parts(declared);
  }
  // `CONTROL_SIZES[size]` and `recipe.base` reach every value of the object.
  if (
    ts.isPropertyAccessExpression(node) ||
    ts.isElementAccessExpression(node)
  ) {
    return stringsOf(node.expression, consts, seen);
  }
  return [];
};

/**
 * Every name a file binds to a value, anywhere in it.
 *
 * A class recipe sits at the top level in `button.tsx`, inside the component
 * function in `material-swatch.tsx`, and in a destructured parameter default
 * in `code-block.tsx`. All three are read the same way here. A name bound
 * twice keeps both values, because the fixture records what can land on a
 * slot rather than what lands on one render.
 *
 * Imports are not followed. `control-trigger.ts` and `row-link.ts` have their
 * own fixture entries, and the Svelte side reads a component's own folder by
 * the same rule.
 *
 * @param {import('typescript').SourceFile} source
 * @returns {Map<string, Expression[]>}
 */
const constantsOf = (source) => {
  /** @type {Map<string, Expression[]>} */
  const found = new Map();
  /** @param {Node} node */
  const visit = (node) => {
    const bound =
      ts.isVariableDeclaration(node) || ts.isBindingElement(node)
        ? node
        : undefined;
    if (bound?.initializer && ts.isIdentifier(bound.name)) {
      const under = found.get(bound.name.text) ?? [];
      under.push(bound.initializer);
      found.set(bound.name.text, under);
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return found;
};

/**
 * @param {import('typescript').JsxOpeningLikeElement} element
 * @param {string} name
 * @returns {string | undefined}
 */
const literalAttribute = (element, name) => {
  const attribute = element.attributes.properties
    .filter((candidate) => ts.isJsxAttribute(candidate))
    .find((candidate) => candidate.name.getText() === name);
  const value = attribute?.initializer;
  return value && ts.isStringLiteral(value) ? value.text : undefined;
};

/**
 * `className`, and the `*ClassName` props a component uses to push classes
 * into a part it renders itself.
 *
 * @param {string} name
 * @returns {boolean}
 */
const isClassAttribute = (name) =>
  name === 'className' || name.endsWith('ClassName');

/**
 * @param {string} path
 * @returns {Record<string, string[]>}
 */
const extract = (path) => {
  const text = readFileSync(path, 'utf8');
  const component = basename(path).replace(/\.tsx?$/, '');
  const source = ts.createSourceFile(
    path,
    text,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  const consts = constantsOf(source);
  /** @type {Map<string, Set<string>>} */
  const slots = new Map();

  /**
   * @param {string} slot
   * @param {string[]} classes
   * @returns {void}
   */
  const record = (slot, classes) => {
    if (classes.length === 0) return;
    const into = slots.get(slot) ?? new Set();
    for (const name of classes) into.add(name);
    slots.set(slot, into);
  };

  /**
   * @param {Node} node
   * @param {string} slot
   * @returns {void}
   */
  const walk = (node, slot) => {
    let inner = slot;
    /** @type {import('typescript').JsxOpeningLikeElement | undefined} */
    let opening = undefined;
    if (ts.isJsxElement(node)) opening = node.openingElement;
    else if (ts.isJsxSelfClosingElement(node)) opening = node;
    // `cloneElement(child, { className: ... })` in field.tsx puts a class on a
    // control it does not render itself, so the class never reaches a JSX
    // attribute here.
    if (
      ts.isPropertyAssignment(node) &&
      isClassAttribute(node.name.getText())
    ) {
      record(slot, stringsOf(node.initializer, consts).flatMap(names));
    }
    if (opening) {
      inner = literalAttribute(opening, 'data-slot') ?? slot;
      const attributes = opening.attributes.properties.filter((property) =>
        ts.isJsxAttribute(property),
      );
      for (const attribute of attributes) {
        if (!isClassAttribute(attribute.name.getText())) continue;
        const value = attribute.initializer;
        const expression =
          value && ts.isJsxExpression(value) ? value.expression : value;
        record(inner, stringsOf(expression, consts).flatMap(names));
      }
    }
    ts.forEachChild(node, (child) => {
      walk(child, inner);
    });
  };

  // `control-trigger.ts` and `row-link.ts` are class recipes with no markup.
  // Their exported strings are the whole entry.
  if (path.endsWith('.ts')) {
    for (const statement of source.statements) {
      if (!ts.isVariableStatement(statement)) continue;
      const exported = ts
        .getModifiers(statement)
        ?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword);
      if (exported !== true) continue;
      for (const declaration of statement.declarationList.declarations) {
        record(
          component,
          stringsOf(declaration.initializer, consts).flatMap(names),
        );
      }
    }
  } else {
    walk(source, component);
  }

  /** @type {Record<string, string[]>} */
  const entry = {};
  const ordered = [...slots].sort(([left], [right]) =>
    left.localeCompare(right),
  );
  for (const [slot, classes] of ordered) entry[slot] = [...classes].sort();
  return entry;
};

const wanted = only ? new Set(only.split(',')) : undefined;
const files = readdirSync(uiDir)
  .filter((name) => /\.tsx?$/.test(name))
  .filter((name) => !name.includes('.stories.') && !name.includes('.test.'))
  .filter((name) => !wanted || wanted.has(name.replace(/\.tsx?$/, '')))
  .sort();

/** @type {Record<string, Record<string, string[]>>} */
const fixture = {};
for (const name of files) {
  fixture[name.replace(/\.tsx?$/, '')] = extract(join(uiDir, name));
}

const json = `${JSON.stringify(fixture, null, 2)}\n`;
if (wanted) process.stdout.write(json);
else writeFileSync(outPath, json);
