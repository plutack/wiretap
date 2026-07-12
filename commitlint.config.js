// commitlint configuration for wiretap.
//
// We inherit @commitlint/config-conventional (the official Angular-style
// rules) and layer on a few project-specific tweaks:
//   * type-enum lists every type we accept; new types go here.
//   * scope-enum is a hint list but not strictly enforced (subject-enum
//     would reject unknown scopes; we want scopes to remain optional and
//     open-ended as the package set grows).
//   * header-max-length is set high enough for descriptive subjects without
//     overflowing `git log --oneline` viewports.
//
// See https://conventionalcommits.org for the spec and
// https://commitlint.js.org/reference/rules for the rule reference.
module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'type-enum': [
      2,
      'always',
      [
        'feat',     // new feature
        'fix',      // bug fix
        'docs',     // docs only (PLAN.md, README, CONTRIBUTING)
        'style',    // formatting, no code change
        'refactor', // code change that neither adds a feature nor fixes a bug
        'perf',     // performance improvement
        'test',     // adding or correcting tests
        'build',    // build system, dependencies, wails scaffolding
        'ci',       // CI configuration
        'chore',    // misc; no src/test changes
        'revert',   // reverting a prior commit
      ],
    ],
    // Scopes are conventional but not enforced to a closed set; we list the
    // ones we expect so reviewers can spot typos. As the package set grows
    // this list will too. Severity is 1 (warning), so out-of-list scopes
    // print a hint instead of blocking the commit.
    'scope-enum': [
      1,
      'always',
      [
        'api',
        'cli',
        'config',
        'intercept',
        'relayproto',
        'relayd',
        'relayclient',
        'store',
        'shellscript',
        'castore',
        'tui',
        'testutil',
        'wiretap',
        'wiretap-relay',
        'release',
        'deps',
        'docs',
      ],
    ],
    'header-max-length': [2, 'always', 100],
    'body-max-line-length': [1, 'always', 120],
    'footer-max-line-length': [1, 'always', 120],
  },
};