// Jest jsdom polyfills: jsdom lacks TextEncoder/TextDecoder which
// @grafana/data (via rxjs/util) requires.
// eslint-disable-next-line @typescript-eslint/no-namespace
declare const global: any;
const { TextEncoder, TextDecoder } = require('util');
if (typeof global.TextEncoder === 'undefined') {
  global.TextEncoder = TextEncoder;
}
if (typeof global.TextDecoder === 'undefined') {
  global.TextDecoder = TextDecoder;
}
require('@testing-library/jest-dom');

export {};
