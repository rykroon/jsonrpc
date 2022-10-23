import typing

from starlette.background import BackgroundTask
from starlette.responses import JSONResponse


class SuccessResponse(JSONResponse):

    def __init__(
        self,
        result: typing.Any,
        *,
        jsonrpc: str = "2.0",
        id: str | int | None = None,
        status_code: int = 200,
        headers: dict[str, str] | None = None,
        media_type: str | None = None,
        background: BackgroundTask | None = None

    ):
        content = {
            'jsonrpc': jsonrpc,
            'result': result,
            'id': id
        }
        super().__init__(
            content=content,
            status_code=status_code,
            headers=headers,
            media_type=media_type,
            background=background
        )


class ErrorResponse(JSONResponse):
    def __init__(
        self,
        error_code: int,
        error_message: str,
        error_data: dict[str, typing.Any] | None = None,
        jsonrpc: str = "2.0",
        id: str | int | None = None,
        status_code: int = 200,
        headers: dict[str, str] | None = None,
        media_type: str | None = None,
        background: BackgroundTask | None = None
    ):
        content = {
            'jsonrpc': jsonrpc,
            'error': {
                'code': error_code,
                'message': error_message,
                'data': error_datap
            },
            'id': id
        }
        super().__init__(
            content=content,
            status_code=status_code,
            headers=headers,
            media_type=media_type,
            background=background
        )