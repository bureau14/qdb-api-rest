package com.quasardb.e2e;

import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.ResultSet;
import java.sql.ResultSetMetaData;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.ArrayList;
import java.util.List;
import java.util.Properties;

/**
 * Runs one query through Arrow's Flight SQL JDBC driver and prints two
 * lines: the column names comma-separated, then the row count.
 *
 * <pre>java -jar jdbc-smoke.jar --url jdbc:arrow-flight-sql://host:port --query SQL [--user U --password P]</pre>
 *
 * No user is the anonymous user, so the driver sends no handshake.
 */
public final class Main {
    private Main() {}

    /** The options of the command line, by name. */
    private record Options(String url, String query, String user, String password) {}

    /** Parses args, every option named and taking one value. */
    private static Options parse(String[] args) {
        String url = null, query = null, user = null, password = null;
        for (int i = 0; i + 1 < args.length; i += 2) {
            switch (args[i]) {
                case "--url" -> url = args[i + 1];
                case "--query" -> query = args[i + 1];
                case "--user" -> user = args[i + 1];
                case "--password" -> password = args[i + 1];
                default -> throw new IllegalArgumentException("unknown option " + args[i]);
            }
        }
        if (url == null || query == null || args.length % 2 != 0) {
            throw new IllegalArgumentException("usage: --url URL --query SQL [--user U --password P]");
        }
        return new Options(url, query, user, password);
    }

    /** The connection properties of o. */
    private static Properties propertiesOf(Options o) {
        Properties p = new Properties();
        // TLS with certificate verification off, because every test server
        // runs on the ephemeral certificate, as the ADBC step connects.
        p.setProperty("useEncryption", "true");
        p.setProperty("disableCertificateVerification", "true");
        if (o.user() != null) {
            p.setProperty("user", o.user());
            p.setProperty("password", o.password() == null ? "" : o.password());
        }
        return p;
    }

    public static void main(String[] args) throws SQLException {
        // The tool formats no cell, because cells are compared through the
        // server's encoders in the ADBC step. It prints the names from the
        // result's metadata and counts the rows.
        Options o = parse(args);
        try (Connection c = DriverManager.getConnection(o.url(), propertiesOf(o));
                Statement st = c.createStatement();
                ResultSet rs = st.executeQuery(o.query())) {
            ResultSetMetaData md = rs.getMetaData();
            List<String> names = new ArrayList<>();
            for (int i = 1; i <= md.getColumnCount(); i++) {
                names.add(md.getColumnName(i));
            }
            long rows = 0;
            while (rs.next()) {
                rows++;
            }
            System.out.println(String.join(",", names));
            System.out.println(rows);
        }
    }
}
