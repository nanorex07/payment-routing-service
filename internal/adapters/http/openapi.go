package httpadapter

const openAPISpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Payment Routing Service API",
    "version": "1.0.0",
    "description": "Dynamic payment gateway routing service. Routes payment transactions across Razorpay, PayU, and Cashfree using weighted routing, callback-derived metrics, and a cooldown circuit breaker."
  },
  "servers": [
    {
      "url": "http://localhost:8080",
      "description": "Local development"
    }
  ],
  "tags": [
    {
      "name": "Health",
      "description": "Service health endpoints"
    },
    {
      "name": "Transactions",
      "description": "Payment transaction routing and callback processing"
    }
  ],
  "paths": {
    "/healthz": {
      "get": {
        "tags": ["Health"],
        "summary": "Health check",
        "description": "Returns service health status.",
        "operationId": "healthz",
        "responses": {
          "200": {
            "description": "Service is healthy",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/HealthResponse"
                },
                "examples": {
                  "healthy": {
                    "summary": "Healthy response",
                    "value": {
                      "status": "ok"
                    }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/transactions/initiate": {
      "post": {
        "tags": ["Transactions"],
        "summary": "Initiate a transaction",
        "description": "Creates one local pending transaction, selects a healthy gateway using weighted routing, and rejects duplicate order_id values with 409 Conflict.",
        "operationId": "initiateTransaction",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/InitiateTransactionRequest"
              },
              "examples": {
                "card": {
                  "summary": "Card transaction",
                  "value": {
                    "order_id": "ORD123",
                    "amount": 499.0,
                    "payment_instrument": {
                      "type": "card",
                      "card_number": "****",
                      "expiry": "12/29",
                      "metadata": {
                        "network": "visa"
                      }
                    }
                  }
                }
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Transaction created",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Transaction"
                },
                "examples": {
                  "created": {
                    "summary": "Pending transaction",
                    "value": {
                      "transaction_id": "txn_1f2e3d4c",
                      "order_id": "ORD123",
                      "amount": 499.0,
                      "payment_instrument": {
                        "type": "card",
                        "card_number": "****",
                        "expiry": "12/29",
                        "metadata": {
                          "network": "visa"
                        }
                      },
                      "gateway": "razorpay",
                      "status": "pending",
                      "created_at": "2026-05-06T00:00:00Z",
                      "updated_at": "2026-05-06T00:00:00Z"
                    }
                  }
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/BadRequest"
          },
          "409": {
            "description": "A transaction already exists for this order_id",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ErrorResponse"
                },
                "examples": {
                  "duplicateOrder": {
                    "summary": "Duplicate order",
                    "value": {
                      "error": "duplicate_order",
                      "message": "transaction already exists for order"
                    }
                  }
                }
              }
            }
          },
          "503": {
            "description": "No healthy gateway is available",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ErrorResponse"
                },
                "examples": {
                  "noHealthyGateway": {
                    "summary": "No healthy gateway",
                    "value": {
                      "error": "no_healthy_gateway",
                      "message": "no healthy gateway available"
                    }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/transactions/callback": {
      "post": {
        "tags": ["Transactions"],
        "summary": "Process gateway callback",
        "description": "Updates a transaction status and records success/failure metrics. Every payload must include gateway; the gateway value selects provider-specific parsing.",
        "operationId": "processTransactionCallback",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "oneOf": [
                  {
                    "$ref": "#/components/schemas/GenericCallbackRequest"
                  },
                  {
                    "$ref": "#/components/schemas/RazorpayCallbackRequest"
                  },
                  {
                    "$ref": "#/components/schemas/PayUCallbackRequest"
                  },
                  {
                    "$ref": "#/components/schemas/CashfreeCallbackRequest"
                  }
                ]
              },
              "examples": {
                "genericSuccess": {
                  "summary": "Generic success callback",
                  "value": {
                    "transaction_id": "txn_replace_me",
                    "order_id": "ORD123",
                    "gateway": "razorpay",
                    "status": "success"
                  }
                },
                "genericFailure": {
                  "summary": "Generic failure callback",
                  "value": {
                    "transaction_id": "txn_replace_me",
                    "order_id": "ORD123",
                    "gateway": "razorpay",
                    "status": "failure",
                    "reason": "Customer Cancelled"
                  }
                },
                "razorpay": {
                  "summary": "Razorpay provider callback",
                  "value": {
                    "gateway": "razorpay",
                    "transaction_id": "txn_replace_me",
                    "razorpay_order_id": "ORD123",
                    "event": "payment.captured"
                  }
                },
                "payu": {
                  "summary": "PayU provider callback",
                  "value": {
                    "gateway": "payu",
                    "transaction_id": "txn_replace_me",
                    "txnid": "ORD123",
                    "unmappedstatus": "failed",
                    "field9": "Bank declined"
                  }
                },
                "cashfree": {
                  "summary": "Cashfree provider callback",
                  "value": {
                    "gateway": "cashfree",
                    "transaction_id": "txn_replace_me",
                    "orderId": "ORD123",
                    "txStatus": "SUCCESS",
                    "txMsg": "Payment completed"
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Callback processed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CallbackResponse"
                },
                "examples": {
                  "success": {
                    "summary": "Updated transaction and metrics snapshot",
                    "value": {
                      "transaction": {
                        "transaction_id": "txn_1f2e3d4c",
                        "order_id": "ORD123",
                        "amount": 499.0,
                        "gateway": "razorpay",
                        "status": "success",
                        "created_at": "2026-05-06T00:00:00Z",
                        "updated_at": "2026-05-06T00:00:10Z"
                      },
                      "metrics": {
                        "gateway": "razorpay",
                        "successes": 1,
                        "failures": 0,
                        "total": 1,
                        "success_rate": 1.0,
                        "healthy": true,
                        "reason": "healthy"
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/BadRequest"
          },
          "404": {
            "description": "Transaction not found",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ErrorResponse"
                },
                "examples": {
                  "notFound": {
                    "summary": "Missing transaction",
                    "value": {
                      "error": "transaction_not_found",
                      "message": "transaction not found"
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  },
  "components": {
    "responses": {
      "BadRequest": {
        "description": "Invalid request payload",
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/ErrorResponse"
            },
            "examples": {
              "badRequest": {
                "summary": "Bad request",
                "value": {
                  "error": "bad_request",
                  "message": "invalid request"
                }
              }
            }
          }
        }
      }
    },
    "schemas": {
      "HealthResponse": {
        "type": "object",
        "required": ["status"],
        "properties": {
          "status": {
            "type": "string",
            "example": "ok"
          }
        }
      },
      "InitiateTransactionRequest": {
        "type": "object",
        "required": ["order_id", "amount", "payment_instrument"],
        "properties": {
          "order_id": {
            "type": "string",
            "example": "ORD123"
          },
          "amount": {
            "type": "number",
            "format": "double",
            "minimum": 0,
            "exclusiveMinimum": true,
            "example": 499.0
          },
          "payment_instrument": {
            "$ref": "#/components/schemas/PaymentInstrument"
          }
        }
      },
      "PaymentInstrument": {
        "type": "object",
        "required": ["type"],
        "properties": {
          "type": {
            "type": "string",
            "example": "card"
          },
          "card_number": {
            "type": "string",
            "example": "****"
          },
          "expiry": {
            "type": "string",
            "example": "12/29"
          },
          "metadata": {
            "type": "object",
            "additionalProperties": true,
            "example": {
              "network": "visa"
            }
          }
        }
      },
      "Transaction": {
        "type": "object",
        "required": ["transaction_id", "order_id", "amount", "gateway", "status", "created_at", "updated_at"],
        "properties": {
          "transaction_id": {
            "type": "string",
            "example": "txn_1f2e3d4c"
          },
          "order_id": {
            "type": "string",
            "example": "ORD123"
          },
          "amount": {
            "type": "number",
            "format": "double",
            "example": 499.0
          },
          "payment_instrument": {
            "$ref": "#/components/schemas/PaymentInstrument"
          },
          "gateway": {
            "$ref": "#/components/schemas/GatewayName"
          },
          "status": {
            "$ref": "#/components/schemas/TransactionStatus"
          },
          "reason": {
            "type": "string",
            "example": "Customer Cancelled"
          },
          "created_at": {
            "type": "string",
            "format": "date-time"
          },
          "updated_at": {
            "type": "string",
            "format": "date-time"
          }
        }
      },
      "GenericCallbackRequest": {
        "type": "object",
        "required": ["order_id", "gateway", "status"],
        "properties": {
          "transaction_id": {
            "type": "string",
            "example": "txn_replace_me"
          },
          "order_id": {
            "type": "string",
            "example": "ORD123"
          },
          "gateway": {
            "$ref": "#/components/schemas/GatewayName"
          },
          "status": {
            "$ref": "#/components/schemas/CallbackStatus"
          },
          "reason": {
            "type": "string",
            "example": "Customer Cancelled"
          }
        }
      },
      "RazorpayCallbackRequest": {
        "type": "object",
        "required": ["gateway", "razorpay_order_id", "event"],
        "properties": {
          "gateway": {
            "type": "string",
            "enum": ["razorpay"]
          },
          "transaction_id": {
            "type": "string",
            "example": "txn_replace_me"
          },
          "razorpay_order_id": {
            "type": "string",
            "example": "ORD123"
          },
          "event": {
            "type": "string",
            "example": "payment.captured"
          },
          "error_reason": {
            "type": "string",
            "example": "Customer Cancelled"
          }
        }
      },
      "PayUCallbackRequest": {
        "type": "object",
        "required": ["gateway", "txnid", "unmappedstatus"],
        "properties": {
          "gateway": {
            "type": "string",
            "enum": ["payu"]
          },
          "transaction_id": {
            "type": "string",
            "example": "txn_replace_me"
          },
          "txnid": {
            "type": "string",
            "example": "ORD123"
          },
          "unmappedstatus": {
            "type": "string",
            "example": "failed"
          },
          "field9": {
            "type": "string",
            "example": "Bank declined"
          }
        }
      },
      "CashfreeCallbackRequest": {
        "type": "object",
        "required": ["gateway", "orderId", "txStatus"],
        "properties": {
          "gateway": {
            "type": "string",
            "enum": ["cashfree"]
          },
          "transaction_id": {
            "type": "string",
            "example": "txn_replace_me"
          },
          "orderId": {
            "type": "string",
            "example": "ORD123"
          },
          "txStatus": {
            "type": "string",
            "example": "SUCCESS"
          },
          "txMsg": {
            "type": "string",
            "example": "Payment completed"
          }
        }
      },
      "CallbackResponse": {
        "type": "object",
        "required": ["transaction", "metrics"],
        "properties": {
          "transaction": {
            "$ref": "#/components/schemas/Transaction"
          },
          "metrics": {
            "$ref": "#/components/schemas/MetricsSnapshot"
          }
        }
      },
      "MetricsSnapshot": {
        "type": "object",
        "required": ["gateway", "successes", "failures", "total", "success_rate", "healthy"],
        "properties": {
          "gateway": {
            "$ref": "#/components/schemas/GatewayName"
          },
          "successes": {
            "type": "integer",
            "example": 1
          },
          "failures": {
            "type": "integer",
            "example": 0
          },
          "total": {
            "type": "integer",
            "example": 1
          },
          "success_rate": {
            "type": "number",
            "format": "double",
            "example": 1.0
          },
          "healthy": {
            "type": "boolean",
            "example": true
          },
          "blocked_until": {
            "type": "string",
            "format": "date-time"
          },
          "reason": {
            "type": "string",
            "example": "healthy"
          }
        }
      },
      "GatewayName": {
        "type": "string",
        "enum": ["razorpay", "payu", "cashfree"]
      },
      "TransactionStatus": {
        "type": "string",
        "enum": ["pending", "success", "failure"]
      },
      "CallbackStatus": {
        "type": "string",
        "enum": ["success", "failure"]
      },
      "ErrorResponse": {
        "type": "object",
        "required": ["error", "message"],
        "properties": {
          "error": {
            "type": "string",
            "example": "bad_request"
          },
          "message": {
            "type": "string",
            "example": "invalid request"
          }
        }
      }
    }
  }
}`

const docsHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Payment Routing Service API Docs</title>
  <style>
    body {
      margin: 0;
    }
    redoc {
      display: block;
      min-height: 100vh;
    }
  </style>
</head>
<body>
  <redoc spec-url="/openapi.json"></redoc>
  <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"> </script>
</body>
</html>`
