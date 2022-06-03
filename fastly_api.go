package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fastly/go-fastly/v6/fastly"
)

type Counter struct {
	id     string
	number int
}

type CsvBuild struct {
	ServiceName string
	ServiceID   string
	Origin      []string
	CreatedDate time.Time
	UpdatedDate time.Time
	Requests    uint64
	Status400   uint64
	Status401   uint64
	Status403   uint64
	Status404   uint64
	Status500   uint64
	Status501   uint64
	Status502   uint64
	Status503   uint64
	Status504   uint64
	Status505   uint64
	HitRatio    float64
	IPsFound    []string
	Domains     []string
	NameServers []string
}

func resolveHostname(hostname string) []string {
	var ipsFound []string
	ips, err := net.LookupIP(hostname)
	if err != nil {
		ipsFound = append(ipsFound, "Host resolution failed")
	}
	for _, ip := range ips {
		ipsFound = append(ipsFound, ip.String())
	}
	return ipsFound
}

func extractCNAME(hostname string) (string, error) {
	var placeHolder string
	possCNAME, err := net.LookupCNAME(hostname)
	if err != nil {
		return placeHolder, err
	}
	return possCNAME, nil
}

func extractNS(domains []string) []string {
	var NameSrvsFoundSlice []string
	NameSrvsFound := make(map[string]bool)
	for _, i := range domains {
		nsFound, err := net.LookupNS(i)
		if err != nil {
			// search for CNAME and return that if found
			cnameFound, err := extractCNAME(i)
			if err != nil {
				NameSrvsFound["NS resolution failed"] = true
			} else {
				NameSrvsFound[cnameFound] = true
			}
			// NameSrvsFound = append(NameSrvsFound, "NS resolution failed")
		}
		for _, nmSrv := range nsFound {
			// NameSrvsFound = append(NameSrvsFound, nmSrv.Host)
			NameSrvsFound[nmSrv.Host] = true
		}
	}
	for k := range NameSrvsFound {
		NameSrvsFoundSlice = append(NameSrvsFoundSlice, k)
	}
	return NameSrvsFoundSlice
}

func extractServiceInfo(client *fastly.Client) (map[string]*Counter, error) {
	m := map[string]*Counter{}
	// Extract list of Service IDs
	serviceIDsExtracted, err := client.ListServices(&fastly.ListServicesInput{})
	if err != nil {
		return nil, err
	}
	for _, i := range serviceIDsExtracted {
		for _, y := range i.Versions {
			c := &Counter{id: i.ID, number: y.Number}
			m[i.Name] = c
		}
	}
	return m, nil
}

func extractStatsInfo(client *fastly.Client, id string) (*fastly.StatsResponse, error) {
	// var results uint64
	fromString := "two months ago"
	byString := "day"
	statsInfo, err := client.GetStats(&fastly.GetStatsInput{
		Service: id,
		From:    fromString,
		By:      byString,
	})
	if err != nil {
		return nil, err
	}
	// for _, i := range statsInfo.Data {
	// 	return i.Requests
	// }
	return statsInfo, nil
}

func extractDomainInfo(client *fastly.Client, id string) []string {
	domainsFound := []string{}
	domainInfo, err := client.ListServiceDomains(&fastly.ListServiceDomainInput{
		ID: id,
	})
	if err != nil {
		domainsFound = append(domainsFound, "No Domains Found")
	}
	for _, i := range domainInfo {
		domainsFound = append(domainsFound, i.Name)
	}
	return domainsFound
}

func s3DomainCheck(s []string) bool {
	for _, d := range s {
		e := strings.Split(d, ".")
		for _, a := range e {
			if a == "s3" {
				return true
			}
		}
	}
	return false
}

func extractOrigin(client *fastly.Client, serviceInfo map[string]*Counter) {
	s := []*CsvBuild{}
	var wg sync.WaitGroup
	mu := &sync.Mutex{}
	for k, v := range serviceInfo {
		wg.Add(1)
		go func(k string, v *Counter, client *fastly.Client, mu *sync.Mutex) {
			defer wg.Done()
			// extract origin, created time, updated time
			backendInfo, err := client.ListBackends(&fastly.ListBackendsInput{
				ServiceID:      v.id,
				ServiceVersion: v.number,
			})
			if err != nil {
				log.Fatal(err)
			}

			domainsFound := extractDomainInfo(client, v.id)
			fastlyStats, err := extractStatsInfo(client, v.id)
			csv_build := &CsvBuild{}
			csv_build.ServiceName = k
			csv_build.ServiceID = v.id
			csv_build.Domains = domainsFound
			if err == nil {
				for _, fstats := range fastlyStats.Data {
					csv_build.HitRatio = fstats.HitRatio
					csv_build.Requests = fstats.Requests
					csv_build.Status400 = fstats.Status400
					csv_build.Status401 = fstats.Status401
					csv_build.Status403 = fstats.Status403
					csv_build.Status404 = fstats.Status404
					csv_build.Status500 = fstats.Status400
					csv_build.Status501 = fstats.Status501
					csv_build.Status502 = fstats.Status502
					csv_build.Status503 = fstats.Status503
					csv_build.Status504 = fstats.Status504
					csv_build.Status505 = fstats.Status505
				}
			}
			csv_build.NameServers = extractNS(domainsFound)
			//csv_build.Requests = requestsFound
			if len(backendInfo) > 0 {
				for _, i := range backendInfo {
					if i.Hostname != "" {
						csv_build.Origin = append(csv_build.Origin, i.Hostname)
						csv_build.CreatedDate = *i.CreatedAt
						csv_build.UpdatedDate = *i.UpdatedAt
						hostExtract := resolveHostname(i.Hostname)
						for _, host := range hostExtract {
							csv_build.IPsFound = append(csv_build.IPsFound, host)
						}
					}
				}
			}
			mu.Lock()
			s = append(s, csv_build)
			mu.Unlock()
		}(k, v, client, mu)
	}
	wg.Wait()
	sort.Slice(s, func(i, j int) bool {
		return s[i].Requests > uint64(s[j].Requests)
	})
	// fmt.Printf("service name,requests,origin,created date,last updated,ip,domains,name-servers\n")
	fmt.Printf("service name,origin,domains,hit-ratio,400status,401status,403status,404status,500status,501status,502status,503status,504status,505status\n")
	for _, t := range s {
		// if t.Requests <= 0 {
		// 	fmt.Printf("%s\n", t.ServiceName)
		// }
		// fmt.Printf("%s,%d,%s,%s,%s,%s,%s,%s\n", t.ServiceName, t.Requests, t.Origin, t.CreatedDate, t.UpdatedDate, t.IPsFound, t.Domains, t.NameServers)
		// if len(t.Domains) > 1 && s3DomainCheck(t.Domains) {
		// 	// if t.Requests >= 1 {
		fmt.Printf("%s,%s,%s,%f,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d\n", t.ServiceName, t.Origin, t.Domains, t.HitRatio, t.Status400, t.Status401, t.Status403, t.Status404, t.Status500, t.Status501, t.Status502, t.Status503, t.Status504, t.Status505)
		// 	// fmt.Printf("%s,%s,%s,%s\n", t.ServiceName, t.ServiceID, t.Origin, t.Domains)
		// 	fmt.Printf("%s - %s\n", t.ServiceName, t.ServiceID)
		// }
	}
}

type Username struct {
	login     string
	name      string
	updatedAt time.Time
	role      string
}

func extractUsers(client *fastly.Client, CustomerID string) ([]Username, error) {
	// create the ListCustomerUsersInput type
	superusers := []Username{}
	usersExtracted, err := client.ListCustomerUsers(&fastly.ListCustomerUsersInput{CustomerID: CustomerID})
	if err != nil {
		print("ListCustomerUsersInput Error:", err.Error())
		return superusers, err
	}
	for _, i := range usersExtracted {
		superusers = append(superusers, Username{login: i.Login, name: i.Name, updatedAt: *i.UpdatedAt, role: i.Role})
	}

	return superusers, err
}

type Version struct {
	Number    int        `mapstructure:"number"`
	Comment   string     `mapstructure:"comment"`
	ServiceID string     `mapstructure:"service_id"`
	Active    bool       `mapstructure:"active"`
	Locked    bool       `mapstructure:"locked"`
	Deployed  bool       `mapstructure:"deployed"`
	Staging   bool       `mapstructure:"staging"`
	Testing   bool       `mapstructure:"testing"`
	CreatedAt *time.Time `mapstructure:"created_at"`
	UpdatedAt *time.Time `mapstructure:"updated_at"`
	DeletedAt *time.Time `mapstructure:"deleted_at"`
}

type Service struct {
	ID            string     `mapstructure:"id"`
	Name          string     `mapstructure:"name"`
	Type          string     `mapstructure:"type"`
	Comment       string     `mapstructure:"comment"`
	CustomerID    string     `mapstructure:"customer_id"`
	CreatedAt     *time.Time `mapstructure:"created_at"`
	UpdatedAt     *time.Time `mapstructure:"updated_at"`
	DeletedAt     *time.Time `mapstructure:"deleted_at"`
	ActiveVersion uint       `mapstructure:"version"`
	Versions      []*Version `mapstructure:"versions"`
}

func extractServiceIds(client *fastly.Client) ([]string, error) {
	serviceIds := []string{}
	listServicesInput := &fastly.ListServicesInput{}
	services, err := client.ListServices(listServicesInput)
	if err != nil {
		print("ListCustomerUsersInput Error:", err.Error())
		return serviceIds, err
	}
	for _, i := range services {
		serviceIds = append(serviceIds, i.ID)
		//fmt.Printf("%s, %s, %s, %d\n", i.ID, i.Name, i.Type,  i.ActiveVersion)
	}
	return serviceIds, err
}

func getServiceDetails(client *fastly.Client, serviceId string) (*fastly.ServiceDetail, error) {
	getServiceInput := &fastly.GetServiceInput{ID: serviceId}
	service, err := client.GetServiceDetails(getServiceInput)
	if err != nil {
		print("GetServiceDetails Error:", err.Error())
		return service, err
	}

	/*if service.ActiveVersion.Active == false {
	  fmt.Printf("%s, %-26s, active:%t, lastUpdated:%s\n", service.ID, service.Name, service.ActiveVersion.Active, service.UpdatedAt)
	}*/

	return service, err

}

// Not working, api is not returning the stats
func getStats(client *fastly.Client, serviceId string) (*fastly.StatsResponse, error) {
	// get stats from 1 year back 1/1/2021:1609477200, 6/3/2021:1622765112, 6/3/2022:1654301141
	getStatsInput := &fastly.GetStatsInput{Service: serviceId, By: "1 min", From: "yesterday", Region: "all"}
	serviceStats, err := client.GetStats(getStatsInput)
	if err != nil {
		print("GetStats Error:", err.Error())
		return serviceStats, err
	}

	fmt.Printf("%s, %s\n", serviceStats.Meta, serviceStats.Data)

	return serviceStats, err

}

func main() {
	client, err := fastly.NewClient(os.Getenv("FASTLY_API_TOKEN"))
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	// Create map of service information
	//serviceInfoMap, err := extractServiceInfo(client)
	//if err != nil {
	//	log.Fatal(err)
	//}
	//fmt.Println(serviceInfoMap)
	//extractOrigin(client, serviceInfoMap)

	/*
	   // Extract Users from account
	   //customerID := "3bq6L7CzJJoMXSHHhR5O82" //cash
	   customerID := "7c4eVzDaJtw5HKBqjn7rS4" //square
	   users, err := extractUsers(client, customerID)

	   // sort by updatedAt descending
	   sort.SliceStable(users, func(i, j int) bool{
	     return users[i].updatedAt.After(users[j].updatedAt)
	   })

	   for _, i := range users {
	     fmt.Printf("%s, %s, %s \n", i.updatedAt, i.login, i.role)
	   }
	   print("count ", len(users))
	*/

	// Extract Service Ids from account
	serviceIds, err := extractServiceIds(client)
	//fmt.Println(serviceIds)

	// Get service details
	serviceDetails := []*fastly.ServiceDetail{}
	for _, serviceId := range serviceIds {
		serviceDetail, _ := getServiceDetails(client, serviceId)
		serviceDetails = append(serviceDetails, serviceDetail)
	}

	// sort by UpdatedAt
	sort.SliceStable(serviceDetails, func(i, j int) bool {
		return serviceDetails[i].Version.UpdatedAt.After(*serviceDetails[j].Version.UpdatedAt)
	})

	// print
	fmt.Printf
	for _, service := range serviceDetails {
		if service.ActiveVersion.Active == true {
			fmt.Printf("%s, %-30s, #ofVersions:%-2d, active:%t, lastUpdated:%s\n", service.ID, service.Name, len(service.Versions), service.ActiveVersion.Active, service.UpdatedAt)
		}
	}

	/*
	   // Get number of requests in the past year
	   // Extract Service Ids from account
	   serviceIds, err := extractServiceIds(client)

	   // getStats is not working not returning data
	   for _, serviceId := range serviceIds {
	     getStats(client, serviceId)
	   }

	*/

}
