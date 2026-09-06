use std::env;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream, ToSocketAddrs};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

#[derive(Clone, Default)]
struct State {
    control_plane: bool,
    prometheus: bool,
    jaeger: bool,
}

fn http_ok(endpoint: &str) -> bool {
    let Some((authority, path)) = endpoint.split_once('/') else { return false };
    let Ok(mut addresses) = authority.to_socket_addrs() else { return false };
    let Some(address) = addresses.next() else { return false };
    let Ok(mut stream) = TcpStream::connect_timeout(&address, Duration::from_secs(1)) else { return false };
    let _ = stream.set_read_timeout(Some(Duration::from_secs(1)));
    let request = format!("GET /{} HTTP/1.0\r\nHost: {}\r\nConnection: close\r\n\r\n", path, authority);
    if stream.write_all(request.as_bytes()).is_err() { return false; }
    let mut response = [0_u8; 64];
    let Ok(size) = stream.read(&mut response) else { return false };
    String::from_utf8_lossy(&response[..size]).starts_with("HTTP/1.0 200")
        || String::from_utf8_lossy(&response[..size]).starts_with("HTTP/1.1 200")
}

fn main() -> std::io::Result<()> {
    let control = env::var("CONTROL_PLANE_ENDPOINT").unwrap_or_else(|_| "stage9-control-plane:17070/healthz".into());
    let prometheus = env::var("PROMETHEUS_ENDPOINT").unwrap_or_else(|_| "prometheus:9090/-/ready".into());
    let jaeger = env::var("JAEGER_ENDPOINT").unwrap_or_else(|_| "jaeger:16686/".into());
    let state = Arc::new(Mutex::new(State::default()));
    let poll_state = Arc::clone(&state);
    thread::spawn(move || loop {
        let next = State {
            control_plane: http_ok(&control),
            prometheus: http_ok(&prometheus),
            jaeger: http_ok(&jaeger),
        };
        *poll_state.lock().expect("validation state poisoned") = next;
        thread::sleep(Duration::from_secs(2));
    });

    let listener = TcpListener::bind("0.0.0.0:19100")?;
    for incoming in listener.incoming() {
        let Ok(mut stream) = incoming else { continue };
        let current = state.lock().expect("validation state poisoned").clone();
        let healthy = current.control_plane && current.prometheus && current.jaeger;
        let body = format!(
            "{{\"healthy\":{},\"control_plane\":{},\"prometheus\":{},\"jaeger\":{}}}\n",
            healthy, current.control_plane, current.prometheus, current.jaeger
        );
        let status = if healthy { "200 OK" } else { "503 Service Unavailable" };
        let response = format!(
            "HTTP/1.1 {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
            status, body.len(), body
        );
        let _ = stream.write_all(response.as_bytes());
    }
    Ok(())
}
