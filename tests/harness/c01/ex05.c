void	ft_putstr(char *str);

int	main(int argc, char **argv)
{
	if (argc < 2)
		return (1);
	ft_putstr(argv[1]);
	return (0);
}
