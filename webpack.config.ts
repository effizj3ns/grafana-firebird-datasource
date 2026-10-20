import path from 'path';
import CopyWebpackPlugin from 'copy-webpack-plugin';
import type { Configuration } from 'webpack';

const config: Configuration = {
  mode: 'production',
  devtool: false,
  // Grafana loads plugins via an AMD loader (require.js-style): module.js
  // must be an AMD module and externals resolve as AMD module requests that
  // Grafana provides at runtime. Bundling the SDK instead would embed its
  // legacy Angular marker strings (setConfigCtrl etc.) and trip Grafana's
  // Angular detector ("angular plugins are not supported"). This mirrors the
  // create-plugin scaffold (library.type 'amd' + string/regex externals,
  // NO externalsType — 'window' would emit window["@grafana/..."] lookups
  // that do not exist in the Grafana runtime).
  externals: [
    { 'amd-module': 'module' },
    'lodash',
    'jquery',
    'moment',
    'rxjs',
    'react',
    'react/jsx-runtime',
    'react-dom',
    'react-redux',
    'redux',
    'slate',
    'slate-plain-serializer',
    '@grafana/slate-react',
    '@emotion/react',
    '@emotion/css',
    'prismjs',
    'i18next',
    'react-router',
    'd3',
    'angular',
    /^@grafana\/ui/i,
    /^@grafana\/runtime/i,
    /^@grafana\/data/i,
  ],
  entry: './src/module.ts',
  output: {
    filename: 'module.js',
    path: path.resolve(process.cwd(), 'dist'),
    clean: {
      keep: /(.*?_(amd64|arm(64)?)(.exe)?|go_plugin_build_manifest)/,
    },
    library: {
      type: 'amd',
    },
  },
  plugins: [
    // Grafana requires plugin.json + logos inside the plugin root (dist/);
    // without them the built artifact is not loadable at all.
    new CopyWebpackPlugin({
      patterns: [
        { from: 'src/plugin.json', to: 'plugin.json' },
        { from: 'src/img', to: 'img' },
        { from: 'README.md', to: 'README.md' },
      ],
    }),
  ],
  resolve: {
    extensions: ['.ts', '.tsx', '.js', '.jsx'],
    extensionAlias: {
      '.js': ['.js', '.ts'],
      '.mjs': ['.mjs', '.js'],
    },
  },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        use: 'ts-loader',
        exclude: /node_modules/,
      },
      {
        test: /\.m?js$/,
        resolve: {
          fullySpecified: false,
        },
      },
      {
        test: /\.css$/,
        use: ['style-loader', 'css-loader'],
      },
      {
        test: /\.(png|jpe?g|gif|svg)$/i,
        type: 'asset/resource',
      },
    ],
  },
};

export default config;
