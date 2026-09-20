#!/usr/bin/env python3
"""
Reliquary MCP Server Package

A Model Context Protocol server that provides access to the Reliquary knowledge management API.
"""

__version__ = "1.1.1"
__author__ = "WeKnora Team"
__description__ = "Reliquary MCP Server - Model Context Protocol server for Reliquary API"

from reliquary_mcp_server import ReliquaryClient, run

__all__ = ["ReliquaryClient", "run"]
