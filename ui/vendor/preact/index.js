// Local entry that wires Preact + htm together so every component imports the
// framework from one place. htm.bind(h) returns a tagged-template `html` that
// renders Preact vnodes with no JSX and no build step.
//
// The bare specifiers "preact" and "preact/hooks" resolve via the import map in
// index.html; htm ships as a self-contained module. All three are vendored
// under ui/vendor (no CDN, no node_modules), embedded via //go:embed all:ui.
import { h, render, Component, Fragment, createRef } from "preact";
import {
  useState,
  useEffect,
  useRef,
  useCallback,
  useMemo,
  useReducer,
} from "preact/hooks";
import htm from "htm";

const html = htm.bind(h);

export {
  html,
  render,
  Component,
  Fragment,
  createRef,
  h,
  useState,
  useEffect,
  useRef,
  useCallback,
  useMemo,
  useReducer,
};
