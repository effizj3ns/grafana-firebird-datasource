import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/data';

export interface FirebirdDataSourceOptions extends DataSourceJsonData {
  host: string;
  port: number;
  database: string;
  user: string;
  dialect: number;
  charset: string;
  maxOpenConns?: number;
  maxIdleConns?: number;
  connMaxLifetimeSec?: number;
  timeoutSec?: number;
  maxRows?: number;
}

export interface FirebirdSecureJsonData {
  password?: string;
}

export interface FirebirdQuery extends DataQuery {
  rawSql: string;
  format: 'table' | 'time_series';
}

export interface FormatOption {
  value: 'table' | 'time_series';
  label: string;
}

export const defaultQuery: Partial<FirebirdQuery> = {
  rawSql: 'SELECT 1 AS value FROM RDB$DATABASE',
  format: 'table',
};
