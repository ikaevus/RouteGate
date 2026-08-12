#!/usr/bin/env node

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = fileURLToPath(new URL('../../', import.meta.url));
const frontendRoot = fileURLToPath(new URL('../', import.meta.url));
const sourceRoot = join(frontendRoot, 'src');
const allowedAttributes = ['placeholder', 'title', 'aria-label', 'alt', 'label'];
const ignoreMarker = 'i18n-check-ignore';

const allowedTerms = new Set([
  'ad',
  'admin',
  'admin ui',
  'android',
  'api',
  'dns',
  'email',
  'grpc',
  'http',
  'https',
  'ip',
  'ipv4',
  'ipv6',
  'ios',
  'json',
  'linux',
  'macos',
  'manager api',
  'mtproto',
  'reality',
  'routegate',
  'sing-box',
  'sni',
  'ssh',
  'tcp',
  'tls',
  'udp',
  'ui',
  'url',
  'uuid',
  'vless',
  'v2box',
  'v2rayn',
  'v2raytun',
  'vmess',
  'vpn',
  'windows',
  'ws',
  'xtls-rprx-vision',
  'xray',
]);

const allowedSingleTokens = new Set([
  'get',
  'post',
  'put',
  'patch',
  'delete',
  'head',
  'options',
  'ok',
  'utc',
]);

function collectSourceFiles(directory) {
  const files = [];

  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry);
    const stats = statSync(path);

    if (stats.isDirectory()) {
      files.push(...collectSourceFiles(path));
      continue;
    }

    if (/\.(ts|tsx)$/.test(entry)) {
      files.push(path);
    }
  }

  return files;
}

function lineNumberForOffset(content, offset) {
  let line = 1;

  for (let index = 0; index < offset; index += 1) {
    if (content.charCodeAt(index) === 10) {
      line += 1;
    }
  }

  return line;
}

function isIgnoredLine(lines, lineNumber) {
  const current = lines[lineNumber - 1] ?? '';
  const previous = lines[lineNumber - 2] ?? '';

  return current.includes(ignoreMarker) || previous.includes(ignoreMarker);
}

function normalizeText(value) {
  return value
    .replace(/&nbsp;/g, ' ')
    .replace(/&amp;/g, '&')
    .replace(/&#39;/g, "'")
    .replace(/&quot;/g, '"')
    .replace(/\s+/g, ' ')
    .trim();
}

function stripAllowedPhrases(value) {
  let remaining = ` ${value.toLowerCase()} `;

  [...allowedTerms]
    .sort((a, b) => b.length - a.length)
    .forEach((term) => {
      const escaped = term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      remaining = remaining.replace(new RegExp(`(?<![a-z0-9-])${escaped}(?![a-z0-9-])`, 'gi'), ' ');
    });

  return remaining;
}

function isLikelyTechnicalOnly(value) {
  const text = normalizeText(value);

  if (text === '') {
    return true;
  }

  if (!/[A-Za-z]/.test(text)) {
    return true;
  }

  if (/^[A-Z0-9_ ./:-]+$/.test(text)) {
    return true;
  }

  if (/^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$/.test(text)) {
    return true;
  }

  if (/^[a-z]{2}(?:_[A-Z]{2})?$/.test(text)) {
    return true;
  }

  if (/^[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}$/.test(text)) {
    return true;
  }

  if (/^(https?:\/\/|\/|#)/.test(text)) {
    return true;
  }

  const withoutAllowedPhrases = stripAllowedPhrases(text)
    .replace(/[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}/g, ' ')
    .replace(/https?:\/\/\S+/g, ' ')
    .replace(/\b\d+(?:\.\d+){1,}\b/g, ' ')
    .replace(/[{}()[\]<>/.,:;|+*=#%!?&'"`~_-]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();

  if (withoutAllowedPhrases === '') {
    return true;
  }

  const words = withoutAllowedPhrases.match(/[A-Za-z][A-Za-z0-9-]*/g) ?? [];

  return words.length > 0 && words.every((word) => allowedSingleTokens.has(word.toLowerCase()));
}

function isCodeLikeSnippet(value) {
  const text = normalizeText(value);

  if (text === '') {
    return true;
  }

  const codePatterns = [
    /\b(?:const|let|return|function|type|interface|new|for|if|else)\b/,
    /=>/,
    /\b[A-Za-z_$][\w$]*\s*\(/,
    /\b[A-Za-z_$][\w$]*\.[A-Za-z_$][\w$]*/,
    /\.\s*(?:length|map|filter|forEach|trim|mutate)\b/,
    /\b(?:number|string|boolean|unknown|Record)\s*(?:\[\]|<)/,
    /(?:&&|\|\||===|!==|>=|<=)/,
    /[()[\]{};]/,
  ];

  if (codePatterns.some((pattern) => pattern.test(text))) {
    return true;
  }

  return /^[\s'",:.\w$-]+:$/.test(text);
}

function reportIssue(issues, filePath, line, kind, value) {
  const text = normalizeText(value);

  if (text === '' || isLikelyTechnicalOnly(text) || isCodeLikeSnippet(text)) {
    return;
  }

  issues.push({
    file: relative(repoRoot, filePath),
    kind,
    line,
    text,
  });
}

function scanFile(filePath) {
  const content = readFileSync(filePath, 'utf8');
  const lines = content.split(/\r?\n/);
  const issues = [];

  if (filePath.endsWith('.tsx')) {
    const jsxTextPattern = />\s*([^<>{}\n][^<>{}]*)\s*</g;
    let match;

    while ((match = jsxTextPattern.exec(content)) !== null) {
      const line = lineNumberForOffset(content, match.index + 1);

      if (!isIgnoredLine(lines, line)) {
        reportIssue(issues, filePath, line, 'JSX text', match[1]);
      }
    }
  }

  const attributePattern = new RegExp(`\\b(${allowedAttributes.join('|')})\\s*=\\s*(["'])(.*?)\\2`, 'gs');
  let attributeMatch;

  while ((attributeMatch = attributePattern.exec(content)) !== null) {
    const line = lineNumberForOffset(content, attributeMatch.index);

    if (!isIgnoredLine(lines, line)) {
      reportIssue(issues, filePath, line, `${attributeMatch[1]} attribute`, attributeMatch[3]);
    }
  }

  return issues;
}

const issues = collectSourceFiles(sourceRoot).flatMap(scanFile);

if (issues.length > 0) {
  console.error('RouteGate frontend i18n check found likely hardcoded user-facing text:\n');

  for (const issue of issues) {
    console.error(`- ${issue.file}:${issue.line} [${issue.kind}] "${issue.text}"`);
  }

  console.error('\nMove user-facing text to frontend/src/shared/i18n/locales/* and render it through t(...).');
  console.error(`For intentional technical-only exceptions, extend frontend/scripts/i18n-check.mjs or add ${ignoreMarker} near the line.`);
  process.exit(1);
}

console.log('RouteGate frontend i18n check passed.');
