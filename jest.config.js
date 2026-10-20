/** @type {import('ts-jest').JestConfigWithTsJest} */
module.exports = {
  preset: 'ts-jest',
  
  testEnvironment: 'jsdom',
  roots: ['<rootDir>/src'],
  setupFilesAfterEnv: ['<rootDir>/jest.setup.ts'],
  transform: {
    '^.+\\.tsx?$': ['ts-jest', { tsconfig: 'tsconfig.test.json' }],
    // ESM-only node_modules (d3-interpolate etc. ship "type": "module"):
    // must be transformed to CJS, otherwise Jest chokes on "export".
    '^.+\\.m?js$': ['ts-jest', { tsconfig: 'tsconfig.json', isolatedModules: true, allowJs: true }],
  },
  moduleNameMapper: {
    '\\.(css|less|scss|sass)$': '<rootDir>/jest.css.mock.js',
    // uplot touches matchMedia at import time; jsdom does not implement it.
    '^uplot$': '<rootDir>/jest.uplot.mock.js',
  },
  transformIgnorePatterns: [
    '/node_modules/(?!((d3-|internmap|react-|ol|@emotion|rxjs|lodash-es)[^/]*))/',
  ],
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx'],
  testMatch: ['**/*.test.ts', '**/*.test.tsx'],
};
