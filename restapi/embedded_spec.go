// Code generated by go-swagger; DO NOT EDIT.

package restapi

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"encoding/json"
)

var (
	// SwaggerJSON embedded version of the swagger document used at generation time
	SwaggerJSON json.RawMessage
	// FlatSwaggerJSON embedded flattened version of the swagger document used at generation time
	FlatSwaggerJSON json.RawMessage
)

func init() {
	SwaggerJSON = json.RawMessage([]byte(`{
  "consumes": [
    "application/json"
  ],
  "produces": [
    "application/json"
  ],
  "schemes": [
    "http",
    "https"
  ],
  "swagger": "2.0",
  "info": {
    "description": "Find out more at https://doc.quasardb.net",
    "title": "QuasarDB API",
    "version": "3.13.0-nightly.0"
  },
  "basePath": "/api",
  "paths": {
    "/cluster": {
      "get": {
        "tags": [
          "cluster"
        ],
        "summary": "Get a summary of the cluster status",
        "operationId": "get-cluster",
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "$ref": "#/definitions/Cluster"
            },
            "examples": {
              "application/json": {
                "diskTotal": 24408800000,
                "diskUsed": 801210000,
                "memoryTotal": 312204568,
                "memoryUsed": 55440601,
                "nodes": [
                  "172.14.0.2:2836",
                  "172.14.0.3:2836"
                ],
                "status": "stable"
              }
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/cluster/nodes/{id}": {
      "get": {
        "tags": [
          "cluster"
        ],
        "summary": "Get information about a single node in the cluster",
        "operationId": "get-node",
        "parameters": [
          {
            "type": "string",
            "description": "The node's id (address and host)",
            "name": "id",
            "in": "path",
            "required": true
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "$ref": "#/definitions/Node"
            },
            "examples": {
              "application/json": {
                "cpuTotal": 2,
                "cpuUsed": 1.84,
                "diskTotal": 99832160000,
                "diskUsed": 360678000,
                "id": "172.14.0.2:2836",
                "memoryTotal": 22857027000,
                "memoryUsed": 2515456400,
                "os": "Linux",
                "quasardbVersion": "1.0.1"
              }
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          },
          "404": {
            "description": "The requested resource could not be found but may be available again in the future.",
            "schema": {
              "type": "string"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/login": {
      "post": {
        "security": [],
        "operationId": "login",
        "parameters": [
          {
            "description": "The user's credential",
            "name": "credential",
            "in": "body",
            "required": true,
            "schema": {
              "$ref": "#/definitions/Credential"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "$ref": "#/definitions/Token"
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/prometheus/read": {
      "post": {
        "security": [],
        "description": "The read endpoint for remote Prometheus storage",
        "consumes": [
          "application/x-protobuf"
        ],
        "produces": [
          "application/x-protobuf"
        ],
        "operationId": "prometheusRead",
        "parameters": [
          {
            "description": "The samples in snappy-encoded protocol buffer format sent from Prometheus",
            "name": "query",
            "in": "body",
            "required": true,
            "schema": {
              "type": "string",
              "format": "binary"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "OK",
            "schema": {
              "type": "string",
              "format": "binary"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/prometheus/write": {
      "post": {
        "security": [],
        "description": "The write endpoint for remote Prometheus storage",
        "consumes": [
          "application/x-protobuf"
        ],
        "produces": [
          "application/x-protobuf"
        ],
        "operationId": "prometheusWrite",
        "parameters": [
          {
            "description": "The samples in snappy-encoded protocol buffer format sent from Prometheus",
            "name": "timeseries",
            "in": "body",
            "required": true,
            "schema": {
              "type": "string",
              "format": "binary"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/query": {
      "post": {
        "tags": [
          "query"
        ],
        "summary": "Query the database",
        "operationId": "post-query",
        "parameters": [
          {
            "name": "query",
            "in": "body",
            "required": true,
            "schema": {
              "$ref": "#/definitions/Query"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "$ref": "#/definitions/QueryResult"
            },
            "examples": {
              "application/json": {
                "tables": [
                  {
                    "columns": [
                      {
                        "data": [
                          "2017-01-01 T00:00:00",
                          "2017-01-01 T00:00:01"
                        ],
                        "name": "timestamps"
                      },
                      {
                        "data": [
                          0,
                          1
                        ],
                        "name": "column_1"
                      }
                    ],
                    "name": "timeseries"
                  }
                ]
              }
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/tables/{name}.csv": {
      "get": {
        "produces": [
          "text/csv"
        ],
        "summary": "Fetch the rows of a table between a given date range and return as csv",
        "operationId": "get-table-csv",
        "parameters": [
          {
            "type": "string",
            "name": "name",
            "in": "path",
            "required": true
          },
          {
            "type": "string",
            "name": "start",
            "in": "query",
            "required": true
          },
          {
            "type": "string",
            "name": "end",
            "in": "query",
            "required": true
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "type": "string",
              "format": "binary"
            }
          },
          "400": {
            "description": "Bad Request."
          },
          "500": {
            "description": "Internal Error."
          }
        }
      }
    },
    "/tags": {
      "get": {
        "tags": [
          "tags"
        ],
        "summary": "Query the database for all tag names",
        "operationId": "get-tags",
        "parameters": [
          {
            "type": "string",
            "name": "regex",
            "in": "query"
          }
        ],
        "responses": {
          "200": {
            "description": "Successful Operation",
            "schema": {
              "$ref": "#/definitions/QueryResult"
            },
            "examples": {
              "application/json": {
                "tables": [
                  {
                    "columns": [
                      {
                        "data": [
                          "tag1",
                          "tag2"
                        ],
                        "name": "tag",
                        "type": "string"
                      }
                    ],
                    "name": "tags"
                  }
                ]
              }
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    }
  },
  "definitions": {
    "Cluster": {
      "type": "object",
      "required": [
        "memoryTotal",
        "memoryUsed",
        "diskTotal",
        "diskUsed",
        "nodes",
        "status"
      ],
      "properties": {
        "diskTotal": {
          "type": "integer"
        },
        "diskUsed": {
          "type": "integer"
        },
        "memoryTotal": {
          "type": "integer"
        },
        "memoryUsed": {
          "type": "integer"
        },
        "nodes": {
          "type": "array",
          "items": {
            "type": "string"
          }
        },
        "status": {
          "type": "string",
          "enum": [
            "stable",
            "unstable",
            "unreachable"
          ]
        }
      }
    },
    "Credential": {
      "type": "object",
      "properties": {
        "secret_key": {
          "type": "string"
        },
        "username": {
          "type": "string"
        }
      }
    },
    "Node": {
      "type": "object",
      "required": [
        "id",
        "os",
        "quasardbVersion",
        "memoryTotal",
        "memoryUsed",
        "diskTotal",
        "diskUsed",
        "cpuTotal",
        "cpuUsed"
      ],
      "properties": {
        "cpuTotal": {
          "type": "integer"
        },
        "cpuUsed": {
          "type": "integer"
        },
        "diskTotal": {
          "type": "integer"
        },
        "diskUsed": {
          "type": "integer"
        },
        "id": {
          "type": "string"
        },
        "memoryTotal": {
          "type": "integer"
        },
        "memoryUsed": {
          "type": "integer"
        },
        "os": {
          "type": "string"
        },
        "quasardbVersion": {
          "type": "string"
        }
      }
    },
    "Principal": {
      "type": "string"
    },
    "QdbError": {
      "properties": {
        "message": {
          "type": "string"
        }
      }
    },
    "Query": {
      "type": "object",
      "properties": {
        "query": {
          "type": "string"
        }
      }
    },
    "QueryColumn": {
      "properties": {
        "data": {
          "type": "array",
          "items": {
            "type": "object"
          }
        },
        "name": {
          "type": "string"
        },
        "type": {
          "type": "string"
        }
      }
    },
    "QueryResult": {
      "type": "object",
      "required": [
        "tables"
      ],
      "properties": {
        "tables": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/QueryTable"
          }
        }
      }
    },
    "QueryTable": {
      "properties": {
        "columns": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/QueryColumn"
          }
        },
        "name": {
          "type": "string"
        }
      }
    },
    "Token": {
      "type": "object",
      "properties": {
        "token": {
          "type": "string"
        }
      }
    }
  },
  "securityDefinitions": {
    "Bearer": {
      "type": "apiKey",
      "name": "Authorization",
      "in": "header"
    },
    "UrlParam": {
      "type": "apiKey",
      "name": "token",
      "in": "query"
    }
  },
  "security": [
    {
      "Bearer": []
    },
    {
      "UrlParam": []
    }
  ],
  "tags": [
    {
      "description": "Operational statistics about the QuasarDB cluster",
      "name": "cluster"
    }
  ]
}`))
	FlatSwaggerJSON = json.RawMessage([]byte(`{
  "consumes": [
    "application/json"
  ],
  "produces": [
    "application/json"
  ],
  "schemes": [
    "http",
    "https"
  ],
  "swagger": "2.0",
  "info": {
    "description": "Find out more at https://doc.quasardb.net",
    "title": "QuasarDB API",
    "version": "3.13.0-nightly.0"
  },
  "basePath": "/api",
  "paths": {
    "/cluster": {
      "get": {
        "tags": [
          "cluster"
        ],
        "summary": "Get a summary of the cluster status",
        "operationId": "get-cluster",
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "$ref": "#/definitions/Cluster"
            },
            "examples": {
              "application/json": {
                "diskTotal": 24408800000,
                "diskUsed": 801210000,
                "memoryTotal": 312204568,
                "memoryUsed": 55440601,
                "nodes": [
                  "172.14.0.2:2836",
                  "172.14.0.3:2836"
                ],
                "status": "stable"
              }
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/cluster/nodes/{id}": {
      "get": {
        "tags": [
          "cluster"
        ],
        "summary": "Get information about a single node in the cluster",
        "operationId": "get-node",
        "parameters": [
          {
            "type": "string",
            "description": "The node's id (address and host)",
            "name": "id",
            "in": "path",
            "required": true
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "$ref": "#/definitions/Node"
            },
            "examples": {
              "application/json": {
                "cpuTotal": 2,
                "cpuUsed": 1.84,
                "diskTotal": 99832160000,
                "diskUsed": 360678000,
                "id": "172.14.0.2:2836",
                "memoryTotal": 22857027000,
                "memoryUsed": 2515456400,
                "os": "Linux",
                "quasardbVersion": "1.0.1"
              }
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          },
          "404": {
            "description": "The requested resource could not be found but may be available again in the future.",
            "schema": {
              "type": "string"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/login": {
      "post": {
        "security": [],
        "operationId": "login",
        "parameters": [
          {
            "description": "The user's credential",
            "name": "credential",
            "in": "body",
            "required": true,
            "schema": {
              "$ref": "#/definitions/Credential"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "$ref": "#/definitions/Token"
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/prometheus/read": {
      "post": {
        "security": [],
        "description": "The read endpoint for remote Prometheus storage",
        "consumes": [
          "application/x-protobuf"
        ],
        "produces": [
          "application/x-protobuf"
        ],
        "operationId": "prometheusRead",
        "parameters": [
          {
            "description": "The samples in snappy-encoded protocol buffer format sent from Prometheus",
            "name": "query",
            "in": "body",
            "required": true,
            "schema": {
              "type": "string",
              "format": "binary"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "OK",
            "schema": {
              "type": "string",
              "format": "binary"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/prometheus/write": {
      "post": {
        "security": [],
        "description": "The write endpoint for remote Prometheus storage",
        "consumes": [
          "application/x-protobuf"
        ],
        "produces": [
          "application/x-protobuf"
        ],
        "operationId": "prometheusWrite",
        "parameters": [
          {
            "description": "The samples in snappy-encoded protocol buffer format sent from Prometheus",
            "name": "timeseries",
            "in": "body",
            "required": true,
            "schema": {
              "type": "string",
              "format": "binary"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/query": {
      "post": {
        "tags": [
          "query"
        ],
        "summary": "Query the database",
        "operationId": "post-query",
        "parameters": [
          {
            "name": "query",
            "in": "body",
            "required": true,
            "schema": {
              "$ref": "#/definitions/Query"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "$ref": "#/definitions/QueryResult"
            },
            "examples": {
              "application/json": {
                "tables": [
                  {
                    "columns": [
                      {
                        "data": [
                          "2017-01-01 T00:00:00",
                          "2017-01-01 T00:00:01"
                        ],
                        "name": "timestamps"
                      },
                      {
                        "data": [
                          0,
                          1
                        ],
                        "name": "column_1"
                      }
                    ],
                    "name": "timeseries"
                  }
                ]
              }
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    },
    "/tables/{name}.csv": {
      "get": {
        "produces": [
          "text/csv"
        ],
        "summary": "Fetch the rows of a table between a given date range and return as csv",
        "operationId": "get-table-csv",
        "parameters": [
          {
            "type": "string",
            "name": "name",
            "in": "path",
            "required": true
          },
          {
            "type": "string",
            "name": "start",
            "in": "query",
            "required": true
          },
          {
            "type": "string",
            "name": "end",
            "in": "query",
            "required": true
          }
        ],
        "responses": {
          "200": {
            "description": "Successful operation",
            "schema": {
              "type": "string",
              "format": "binary"
            }
          },
          "400": {
            "description": "Bad Request."
          },
          "500": {
            "description": "Internal Error."
          }
        }
      }
    },
    "/tags": {
      "get": {
        "tags": [
          "tags"
        ],
        "summary": "Query the database for all tag names",
        "operationId": "get-tags",
        "parameters": [
          {
            "type": "string",
            "name": "regex",
            "in": "query"
          }
        ],
        "responses": {
          "200": {
            "description": "Successful Operation",
            "schema": {
              "$ref": "#/definitions/QueryResult"
            },
            "examples": {
              "application/json": {
                "tables": [
                  {
                    "columns": [
                      {
                        "data": [
                          "tag1",
                          "tag2"
                        ],
                        "name": "tag",
                        "type": "string"
                      }
                    ],
                    "name": "tags"
                  }
                ]
              }
            }
          },
          "400": {
            "description": "Bad Request.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          },
          "500": {
            "description": "Internal Error.",
            "schema": {
              "$ref": "#/definitions/QdbError"
            }
          }
        }
      }
    }
  },
  "definitions": {
    "Cluster": {
      "type": "object",
      "required": [
        "memoryTotal",
        "memoryUsed",
        "diskTotal",
        "diskUsed",
        "nodes",
        "status"
      ],
      "properties": {
        "diskTotal": {
          "type": "integer",
          "minimum": 0
        },
        "diskUsed": {
          "type": "integer",
          "minimum": 0
        },
        "memoryTotal": {
          "type": "integer",
          "minimum": 0
        },
        "memoryUsed": {
          "type": "integer",
          "minimum": 0
        },
        "nodes": {
          "type": "array",
          "items": {
            "type": "string"
          }
        },
        "status": {
          "type": "string",
          "enum": [
            "stable",
            "unstable",
            "unreachable"
          ]
        }
      }
    },
    "Credential": {
      "type": "object",
      "properties": {
        "secret_key": {
          "type": "string"
        },
        "username": {
          "type": "string"
        }
      }
    },
    "Node": {
      "type": "object",
      "required": [
        "id",
        "os",
        "quasardbVersion",
        "memoryTotal",
        "memoryUsed",
        "diskTotal",
        "diskUsed",
        "cpuTotal",
        "cpuUsed"
      ],
      "properties": {
        "cpuTotal": {
          "type": "integer",
          "minimum": 0
        },
        "cpuUsed": {
          "type": "integer",
          "minimum": 0
        },
        "diskTotal": {
          "type": "integer",
          "minimum": 0
        },
        "diskUsed": {
          "type": "integer",
          "minimum": 0
        },
        "id": {
          "type": "string"
        },
        "memoryTotal": {
          "type": "integer",
          "minimum": 0
        },
        "memoryUsed": {
          "type": "integer",
          "minimum": 0
        },
        "os": {
          "type": "string"
        },
        "quasardbVersion": {
          "type": "string"
        }
      }
    },
    "Principal": {
      "type": "string"
    },
    "QdbError": {
      "properties": {
        "message": {
          "type": "string"
        }
      }
    },
    "Query": {
      "type": "object",
      "properties": {
        "query": {
          "type": "string"
        }
      }
    },
    "QueryColumn": {
      "properties": {
        "data": {
          "type": "array",
          "items": {
            "type": "object"
          }
        },
        "name": {
          "type": "string"
        },
        "type": {
          "type": "string"
        }
      }
    },
    "QueryResult": {
      "type": "object",
      "required": [
        "tables"
      ],
      "properties": {
        "tables": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/QueryTable"
          }
        }
      }
    },
    "QueryTable": {
      "properties": {
        "columns": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/QueryColumn"
          }
        },
        "name": {
          "type": "string"
        }
      }
    },
    "Token": {
      "type": "object",
      "properties": {
        "token": {
          "type": "string"
        }
      }
    }
  },
  "securityDefinitions": {
    "Bearer": {
      "type": "apiKey",
      "name": "Authorization",
      "in": "header"
    },
    "UrlParam": {
      "type": "apiKey",
      "name": "token",
      "in": "query"
    }
  },
  "security": [
    {
      "Bearer": []
    },
    {
      "UrlParam": []
    }
  ],
  "tags": [
    {
      "description": "Operational statistics about the QuasarDB cluster",
      "name": "cluster"
    }
  ]
}`))
}
