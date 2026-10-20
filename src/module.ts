import { DataSourcePlugin } from '@grafana/data';
import { DataSource } from './datasource';
import { QueryEditor } from './components/QueryEditor';
import { ConfigEditor } from './components/ConfigEditor';
import { FirebirdQuery, FirebirdDataSourceOptions } from './types';

export const plugin = new DataSourcePlugin<DataSource, FirebirdQuery, FirebirdDataSourceOptions>(DataSource)
  .setQueryEditor(QueryEditor)
  .setConfigEditor(ConfigEditor);
